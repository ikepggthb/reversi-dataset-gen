package dataset

import (
	"context"
	"sync"
	"time"

	"github.com/ikepggthb/reversi-dataset-gen/internal/board"
)

func runAllPositionsPhase(ctx context.Context, cfg Config, phase int, w *phaseWriters, existingHashes map[string]bool, existing int64, opts Options, scope *progressScope) (phaseStats, error) {
	enumStarted := time.Now()
	positions, enum := enumeratePhase(phase)
	enumElapsed := time.Since(enumStarted)
	enum.DuplicateRate = ratio(int64(enum.Duplicates), int64(enum.Leaf))
	target := len(positions)
	jobs := make(chan board.Board)
	results := make(chan result, cfg.SelfPlay.Workers*2)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < cfg.SelfPlay.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			selfPlayWorker(ctx, cfg, jobs, results)
		}()
	}
	go func() {
		defer close(jobs)
		defer func() {
			wg.Wait()
			close(results)
		}()
		for _, b := range positions {
			if existingHashes[b.CanonicalHash()] {
				continue
			}
			select {
			case jobs <- b:
			case <-ctx.Done():
				return
			}
		}
	}()
	var emitted int64 = existing
	var m metrics
	vals := newValueAgg()
	evalStarted := time.Now()
	for r := range results {
		addMetrics(&m, r.metrics)
		if r.err != nil || !r.ok {
			if r.metrics.failed == 0 {
				m.failed++
			}
			continue
		}
		split := chooseSplit(r.hash, cfg.Split)
		own, opp := r.b.BitboardsSideToMove()
		if err := w.write(split, own, opp, r.value, r.hash); err != nil {
			cancel()
			return phaseStats{}, err
		}
		vals.add(int(r.value))
		emitted++
		sendProgress(opts.Progress, cfg, phase, int64(target), emitted, buildStats(cfg, phase, target, emitted, int64(enum.Nodes), int64(enum.Duplicates), m, vals, w.counts), opts, scope)
	}
	if err := ctx.Err(); err != nil && emitted < int64(target) {
		return buildStats(cfg, phase, target, emitted, int64(enum.Nodes), int64(enum.Duplicates), m, vals, w.counts), err
	}
	st := buildStats(cfg, phase, target, emitted, int64(enum.Nodes), int64(enum.Duplicates), m, vals, w.counts)
	st.Schema = "rdg_all_positions_phase_stats_v1"
	st.EnumerationElapsedSec = enumElapsed.Seconds()
	st.EvaluationElapsedSec = time.Since(evalStarted).Seconds()
	st.Enumeration = &enum
	st.Evaluation = &evaluationStats{
		Target: target, Evaluated: emitted, Failed: m.failed,
		SelfPlayGames: m.selfGames, Moves: m.selfMoves, PassCount: m.selfPass,
		EngineTimeouts: m.timeouts, EngineErrors: m.errors,
	}
	return st, nil
}

func selfPlayWorker(ctx context.Context, cfg Config, jobs <-chan board.Board, out chan<- result) {
	eng, err := newEngine(ctx, resolveEngine(cfg, cfg.SelfPlay.AI))
	if err != nil {
		sendResult(ctx, out, result{err: err, metrics: metrics{errors: 1}})
		return
	}
	defer eng.Close()
	for {
		var b board.Board
		var ok bool
		select {
		case b, ok = <-jobs:
			if !ok {
				return
			}
		case <-ctx.Done():
			return
		}
		value, met, err := finishGame(ctx, eng, b.GameText(), b.Player(), cfg.SelfPlay.Retries)
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

func enumeratePhase(phase int) ([]board.Board, enumerationStats) {
	seen := map[string]bool{}
	var out []board.Board
	var stats enumerationStats
	var dfs func(board.Board)
	dfs = func(b board.Board) {
		stats.Nodes++
		if phaseOf(b) == phase {
			stats.Leaf++
			h := b.CanonicalHash()
			if seen[h] {
				stats.Duplicates++
				return
			}
			seen[h] = true
			out = append(out, b)
			stats.Unique++
			return
		}
		status, legal := b.Status()
		switch status {
		case board.StatusGameOver:
			stats.GameOver++
		case board.StatusPass:
			stats.Pass++
			_ = b.Apply(board.PassMove)
			dfs(b)
		case board.StatusPlay:
			for _, sq := range legal {
				nb := b
				_ = nb.Apply(board.Move(sq))
				dfs(nb)
			}
		}
	}
	dfs(board.New())
	return out, stats
}

func estimateAllPositions(ctx context.Context, phase int) (int, string, error) {
	if phase > 9 {
		return 0, " WARNING: phase is deep and may explode", nil
	}
	positions, _ := enumeratePhase(phase)
	return len(positions), "", nil
}
