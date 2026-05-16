package dataset

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func Run(ctx context.Context, cfg Config, opts Options) error {
	started := time.Now()
	if opts.DryRun {
		return dryRun(ctx, cfg)
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return err
	}
	if cfg.Mode == ModeNormal && cfg.Window.Enabled {
		return runWindowedNormal(ctx, cfg, opts, started)
	}
	overallTotal := estimateOverallTotal(cfg)
	var total int64
	splits := map[string]int64{"train": 0, "valid": 0, "test": 0}
	var phaseStats []phaseSummary
	for i, phase := range cfg.Phases {
		scope := progressScope{
			PhaseIndex:   i + 1,
			PhaseCount:   len(cfg.Phases),
			OverallDone:  total,
			OverallTotal: overallTotal,
		}
		st, err := runPhase(ctx, cfg, phase, opts, &scope)
		if err != nil {
			return err
		}
		total += int64(st.EmittedSamples)
		phaseStats = append(phaseStats, phaseSummary{
			Phase:              phase,
			EmittedSamples:     st.EmittedSamples,
			FailedAttempts:     st.FailedAttempts,
			DuplicatePositions: st.DuplicatePositions,
			DuplicateRate:      st.DuplicateRate,
		})
		for k, v := range st.Split {
			splits[k] += v
		}
	}
	sum := summary{
		Schema: "rdg_summary_v1", Mode: cfg.Mode,
		StartedAt: started.Format(time.RFC3339), FinishedAt: time.Now().Format(time.RFC3339),
		ElapsedSec: time.Since(started).Seconds(), Phases: cfg.Phases,
		TotalSamples: total, Split: splits, PhaseStats: phaseStats,
	}
	return writeJSON(filepath.Join(cfg.OutputDir, "summary.json"), sum)
}

func dryRun(ctx context.Context, cfg Config) error {
	for _, phase := range cfg.Phases {
		if cfg.Mode == ModeAllPositions {
			n, warn, err := estimateAllPositions(ctx, phase)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "phase %d: estimated unique positions %d%s\n", phase, n, warn)
			continue
		}
		if cfg.Window.Enabled {
			if windowIncludesPhase(cfg.Window, phase) {
				fmt.Fprintf(os.Stderr, "phase %d: target samples %d (window)\n", phase, cfg.SamplesPerPhase)
			}
			continue
		}
		fmt.Fprintf(os.Stderr, "phase %d: target samples %d\n", phase, cfg.SamplesPerPhase)
	}
	return nil
}

func estimateOverallTotal(cfg Config) int64 {
	if cfg.Mode == ModeNormal {
		if cfg.Window.Enabled {
			return int64(cfg.SamplesPerPhase * len(windowTargetPhases(cfg)))
		}
		return int64(cfg.SamplesPerPhase * len(cfg.Phases))
	}
	var total int64
	for _, phase := range cfg.Phases {
		positions, _ := enumeratePhase(phase)
		total += int64(len(positions))
	}
	return total
}

func ensureNoExistingPhaseOutput(dir string) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "train.rd" || name == "valid.rd" || name == "test.rd" ||
			name == "run_state.json" || name == "metadata.json" || name == "stats.json" || name == "hashes.jsonl" {
			return fmt.Errorf("%s already contains dataset output; use --resume to continue, --resume --repair to rebuild incompatible or incomplete phases, or --force to rebuild all", dir)
		}
	}
	return nil
}

func runPhase(ctx context.Context, cfg Config, phase int, opts Options, scope *progressScope) (phaseStats, error) {
	dir := filepath.Join(cfg.OutputDir, fmt.Sprintf("phase_%02d", phase))
	if opts.Force {
		if err := os.RemoveAll(dir); err != nil {
			return phaseStats{}, err
		}
	}
	if !opts.Force && !opts.Resume {
		if err := ensureNoExistingPhaseOutput(dir); err != nil {
			return phaseStats{}, err
		}
	}
	hasResumeState := false
	if opts.Resume {
		resumeState, ok, err := loadResumeState(dir)
		if err != nil {
			return phaseStats{}, err
		}
		hasResumeState = ok
		if ok && !cfg.SeedSet {
			cfg.Seed = resumeState.Seed
			cfg.ConfigEffectiveHash = effectiveHash(cfg)
		}
		if ok {
			if err := checkResumeMetadata(resumeState, cfg, phase); err != nil {
				if !opts.Repair {
					return phaseStats{}, err
				}
				fmt.Fprintf(os.Stderr, "repair: phase_%02d has incompatible run state (%v); rebuilding this phase\n", phase, err)
				if err := os.RemoveAll(dir); err != nil {
					return phaseStats{}, err
				}
				hasResumeState = false
			}
		}
		if opts.Repair && !hasResumeState {
			exists, err := phaseOutputExists(dir)
			if err != nil {
				return phaseStats{}, err
			}
			if exists {
				fmt.Fprintf(os.Stderr, "repair: phase_%02d has dataset output without run_state.json or metadata.json; rebuilding this phase\n", phase)
				if err := os.RemoveAll(dir); err != nil {
					return phaseStats{}, err
				}
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return phaseStats{}, err
	}
	if !opts.Resume || hasResumeState {
		if err := writeJSON(filepath.Join(dir, "run_state.json"), buildRunState(cfg, phase)); err != nil {
			return phaseStats{}, err
		}
	}
	w, existingHashes, existingCounts, err := openPhaseWriters(dir, opts.Resume)
	if err != nil {
		if !opts.Resume || !opts.Repair {
			return phaseStats{}, err
		}
		fmt.Fprintf(os.Stderr, "repair: phase_%02d has corrupt resume output (%v); rebuilding this phase\n", phase, err)
		if err := os.RemoveAll(dir); err != nil {
			return phaseStats{}, err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return phaseStats{}, err
		}
		hasResumeState = false
		w, existingHashes, existingCounts, err = openPhaseWriters(dir, false)
		if err != nil {
			return phaseStats{}, err
		}
		if err := writeJSON(filepath.Join(dir, "run_state.json"), buildRunState(cfg, phase)); err != nil {
			return phaseStats{}, err
		}
	}
	defer w.close()
	existing := existingCounts["train"] + existingCounts["valid"] + existingCounts["test"]
	if opts.Resume && existing == 0 && !hasResumeState {
		if err := writeJSON(filepath.Join(dir, "run_state.json"), buildRunState(cfg, phase)); err != nil {
			return phaseStats{}, err
		}
		hasResumeState = true
	}
	if opts.Resume && existing > 0 && !hasResumeState {
		return phaseStats{}, fmt.Errorf("phase_%02d: existing records have no run_state.json or metadata.json; use --resume --repair to rebuild only this phase, --force to rebuild all, or delete the phase directory", phase)
	}
	target := cfg.SamplesPerPhase
	if cfg.Mode == ModeAllPositions {
		positions, _ := enumeratePhase(phase)
		target = len(positions)
	}
	if opts.Resume {
		if cfg.Mode == ModeNormal && int(existing) >= cfg.SamplesPerPhase {
			st, err := readStats(filepath.Join(dir, "stats.json"))
			if err == nil {
				sendProgress(opts.Progress, cfg, phase, int64(st.TargetSamples), int64(st.EmittedSamples), st, opts, scope)
				return st, nil
			}
		}
	}
	started := time.Now()
	sendProgress(opts.Progress, cfg, phase, int64(target), existing, phaseStats{}, opts, scope)
	var st phaseStats
	if cfg.Mode == ModeAllPositions {
		if phase > 9 {
			fmt.Fprintf(os.Stderr, "warning: all_positions phase %d may be very large; run --dry-run first for shallow phases and use with care\n", phase)
		}
		st, err = runAllPositionsPhase(ctx, cfg, phase, w, existingHashes, existing, opts, scope)
	} else {
		st, err = runNormalPhase(ctx, cfg, phase, w, existingHashes, existing, opts, scope)
	}
	if err != nil {
		return phaseStats{}, err
	}
	st.StartedAt = started.Format(time.RFC3339)
	st.FinishedAt = time.Now().Format(time.RFC3339)
	st.ElapsedSec = time.Since(started).Seconds()
	fillSpeed(&st)
	if err := w.flush(); err != nil {
		return phaseStats{}, err
	}
	if err := writeJSON(filepath.Join(dir, "stats.json"), st); err != nil {
		return phaseStats{}, err
	}
	if err := writeJSON(filepath.Join(dir, "metadata.json"), buildMetadata(cfg, phase)); err != nil {
		return phaseStats{}, err
	}
	sendProgress(opts.Progress, cfg, phase, int64(st.TargetSamples), int64(st.EmittedSamples), st, opts, scope)
	return st, nil
}
