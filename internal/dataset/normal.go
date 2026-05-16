package dataset

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"

	"github.com/ikepggthb/reversi-dataset-gen/internal/engine"
)

func runNormalPhase(ctx context.Context, cfg Config, phase int, w *phaseWriters, seen map[string]bool, existing int64, opts Options, scope *progressScope) (phaseStats, error) {
	target := cfg.SamplesPerPhase
	need := target - int(existing)
	if need <= 0 {
		return phaseStats{Schema: "rdg_phase_stats_v1", Mode: cfg.Mode, Phase: phase, TargetSamples: target, EmittedSamples: int(existing), Split: w.counts}, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan result, cfg.SelfPlay.Workers*2)
	var attempts atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < cfg.SelfPlay.Workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			normalWorker(ctx, cfg, phase, worker, &attempts, results)
		}(i)
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	var emitted int64 = existing
	var dup int64
	var m metrics
	vals := newValueAgg()
	maxAttempts := int64(cfg.SamplesPerPhase * cfg.Playout.MaxAttemptsMultiplier)
	for r := range results {
		addMetrics(&m, r.metrics)
		if r.err != nil || !r.ok {
			if r.metrics.failed == 0 {
				m.failed++
			}
			if attempts.Load() >= maxAttempts && emitted < int64(target) {
				cancel()
				return buildStats(cfg, phase, target, emitted, attempts.Load(), dup, m, vals, w.counts), fmt.Errorf("phase %d: reached max attempts %d before target %d", phase, maxAttempts, target)
			}
			continue
		}
		if seen[r.hash] {
			dup++
			continue
		}
		seen[r.hash] = true
		split := chooseSplit(r.hash, cfg.Split)
		own, opp := r.b.BitboardsSideToMove()
		if err := w.write(split, own, opp, r.value); err != nil {
			cancel()
			return phaseStats{}, err
		}
		vals.add(int(r.value))
		emitted++
		if emitted >= int64(target) {
			cancel()
			break
		}
		sendProgress(opts.Progress, cfg, phase, int64(target), emitted, buildStats(cfg, phase, target, emitted, attempts.Load(), dup, m, vals, w.counts), opts, scope)
	}
	if err := ctx.Err(); err != nil && emitted < int64(target) {
		return buildStats(cfg, phase, target, emitted, attempts.Load(), dup, m, vals, w.counts), err
	}
	return buildStats(cfg, phase, target, emitted, attempts.Load(), dup, m, vals, w.counts), nil
}

func normalWorker(ctx context.Context, cfg Config, phase, worker int, attempts *atomic.Int64, out chan<- result) {
	rng := rand.New(rand.NewSource(cfg.Seed + int64(worker+1)*1000003))
	var playEng engine.Engine
	if cfg.Playout.AI != nil {
		eng, err := newEngine(ctx, resolveEngine(cfg, *cfg.Playout.AI))
		if err != nil {
			sendResult(ctx, out, result{err: err, metrics: metrics{errors: 1}})
			return
		}
		defer eng.Close()
		playEng = eng
	}
	selfEng, err := newEngine(ctx, resolveEngine(cfg, cfg.SelfPlay.AI))
	if err != nil {
		sendResult(ctx, out, result{err: err, metrics: metrics{errors: 1}})
		return
	}
	defer selfEng.Close()
	for ctx.Err() == nil {
		attempts.Add(1)
		b, met, err := reachPhase(ctx, cfg, phase, rng, playEng)
		if err != nil {
			if !sendResult(ctx, out, result{err: err, metrics: met}) {
				return
			}
			continue
		}
		value, met2, err := finishGame(ctx, selfEng, b.GameText(), b.Player(), cfg.SelfPlay.Retries)
		addMetrics(&met, met2)
		if err != nil {
			if !sendResult(ctx, out, result{err: err, metrics: met}) {
				return
			}
			continue
		}
		if !sendResult(ctx, out, result{b: b, hash: b.CanonicalHash(), value: value, ok: true, metrics: met}) {
			return
		}
	}
}
