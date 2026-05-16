package dataset

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ikepggthb/reversi-dataset-gen/internal/board"
	"github.com/ikepggthb/reversi-dataset-gen/internal/engine"
	"github.com/ikepggthb/reversi-dataset-gen/internal/runui"
)

type datasetWindow struct {
	Anchor int
	Phases []int
}

type windowStatsFile struct {
	Schema                  string             `json:"schema"`
	Size                    int                `json:"size"`
	GeneratedAt             string             `json:"generated_at"`
	TotalTrajectories       int64              `json:"total_trajectories"`
	TotalFailedTrajectories int64              `json:"total_failed_trajectories"`
	TotalExtractedSamples   int64              `json:"total_extracted_samples"`
	TotalDuplicateSamples   int64              `json:"total_duplicate_samples"`
	Windows                 []windowStatsEntry `json:"windows"`
}

type windowStatsEntry struct {
	Anchor             int     `json:"anchor"`
	Phases             []int   `json:"phases"`
	InFlight           int     `json:"in_flight"`
	Trajectories       int64   `json:"trajectories"`
	SelfPlayGames      int64   `json:"self_play_games"`
	SelfPlayMoves      int64   `json:"self_play_moves"`
	SelfPlayPassCount  int64   `json:"self_play_pass_count"`
	EngineTimeouts     int64   `json:"engine_timeouts"`
	EngineErrors       int64   `json:"engine_errors"`
	ExtractedSamples   int64   `json:"extracted_samples"`
	DuplicateSamples   int64   `json:"duplicate_samples"`
	FailedTrajectories int64   `json:"failed_trajectories"`
	ElapsedSeconds     float64 `json:"elapsed_seconds,omitempty"`
}

type windowPhaseState struct {
	phase     int
	index     int
	dir       string
	writers   *phaseWriters
	seen      map[string]bool
	emitted   int64
	existing  int64
	started   time.Time
	attempts  int64
	dup       int64
	extracted int64
	metrics   metrics
	values    *valueAgg
	resumed   bool
}

type windowRunState struct {
	window       datasetWindow
	index        int
	inFlight     int
	started      time.Time
	metrics      metrics
	trajectories int64
	failed       int64
	extracted    int64
	duplicates   int64
}

type windowResult struct {
	window  datasetWindow
	samples map[int]windowSample
	ok      bool
	err     error
	metrics metrics
}

type windowSample struct {
	b     board.Board
	hash  string
	value int16
}

func runWindowedNormal(ctx context.Context, cfg Config, opts Options, started time.Time) error {
	phases := windowTargetPhases(cfg)
	if len(phases) == 0 {
		return errors.New("window has no target phases")
	}
	windows := buildWindows(cfg.Phases, cfg.Window.Size)
	if len(windows) == 0 {
		return errors.New("window has no runnable windows")
	}
	if opts.Resume && !cfg.SeedSet {
		resumeSeed, ok, err := firstResumeSeed(cfg, phases)
		if err != nil {
			return err
		}
		if ok {
			cfg.Seed = resumeSeed
			cfg.ConfigEffectiveHash = effectiveHash(cfg)
		}
	}
	states, err := openWindowPhaseStates(cfg, phases, opts)
	if err != nil {
		return err
	}
	defer func() {
		for _, st := range states {
			if st.writers != nil {
				st.writers.close()
			}
		}
	}()
	overallTotal := int64(cfg.SamplesPerPhase * len(phases))
	var overallDone int64
	for _, st := range states {
		overallDone += min64(st.emitted, int64(cfg.SamplesPerPhase))
	}
	windowStates := buildWindowRunStates(windows)
	sendWindowProgress(opts.Progress, cfg, windowStates, states, 0, overallDone, overallTotal, opts)
	if overallDone >= overallTotal {
		if err := writeWindowStats(cfg, windowStates); err != nil {
			return err
		}
		return writeWindowSummary(cfg, started, states)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan datasetWindow, cfg.SelfPlay.Workers)
	results := make(chan windowResult, cfg.SelfPlay.Workers*2)
	var attempts atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < cfg.SelfPlay.Workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			windowWorker(ctx, cfg, worker, jobs, &attempts, results)
		}(i)
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	jobsClosed := false
	closeJobs := func() {
		if !jobsClosed {
			close(jobs)
			jobsClosed = true
		}
	}
	defer closeJobs()
	maxAttempts := int64(cfg.SamplesPerPhase * len(phases) * cfg.Playout.MaxAttemptsMultiplier)
	phaseSet := map[int]*windowPhaseState{}
	for _, st := range states {
		phaseSet[st.phase] = st
	}
	inFlight := 0
	activeWindowIndex := 0
	enqueue := func() {
		for inFlight < cfg.SelfPlay.Workers && overallDone < overallTotal {
			idx := nextRunnableWindow(windowStates, states, cfg.SamplesPerPhase, &activeWindowIndex)
			if idx < 0 {
				return
			}
			select {
			case jobs <- windowStates[idx].window:
				inFlight++
				windowStates[idx].inFlight++
				sendWindowProgress(opts.Progress, cfg, windowStates, states, idx, overallDone, overallTotal, opts)
			case <-ctx.Done():
				return
			}
		}
	}
	enqueue()
	for inFlight > 0 {
		r, ok := <-results
		if !ok {
			break
		}
		inFlight--
		widx := windowStateIndex(windowStates, r.window.Anchor)
		if widx >= 0 && windowStates[widx].inFlight > 0 {
			windowStates[widx].inFlight--
		}
		if r.err != nil || !r.ok {
			if widx < 0 {
				cancel()
				return r.err
			}
			addMetrics(&windowStates[widx].metrics, r.metrics)
			windowStates[widx].trajectories++
			windowStates[widx].failed++
			if attempts.Load() >= maxAttempts && overallDone < overallTotal {
				cancel()
				return fmt.Errorf("window: reached max attempts %d before target %d", maxAttempts, overallTotal)
			}
			sendWindowProgress(opts.Progress, cfg, windowStates, states, widx, overallDone, overallTotal, opts)
			enqueue()
			continue
		}
		if widx < 0 {
			cancel()
			return fmt.Errorf("window: result for unknown anchor %d", r.window.Anchor)
		}
		addMetrics(&windowStates[widx].metrics, r.metrics)
		windowStates[widx].trajectories++
		for phase, sample := range r.samples {
			st := phaseSet[phase]
			if st == nil || st.emitted >= int64(cfg.SamplesPerPhase) {
				continue
			}
			st.extracted++
			windowStates[widx].extracted++
			if st.seen[sample.hash] {
				st.dup++
				windowStates[widx].duplicates++
				continue
			}
			st.seen[sample.hash] = true
			split := chooseSplit(sample.hash, cfg.Split)
			own, opp := sample.b.BitboardsSideToMove()
			if err := st.writers.write(split, own, opp, sample.value); err != nil {
				cancel()
				return err
			}
			st.values.add(int(sample.value))
			st.emitted++
			overallDone = min64(overallDone+1, overallTotal)
			if overallDone >= overallTotal {
				cancel()
				closeJobs()
				break
			}
		}
		sendWindowProgress(opts.Progress, cfg, windowStates, states, widx, overallDone, overallTotal, opts)
		if overallDone >= overallTotal {
			break
		}
		enqueue()
	}
	if overallDone >= overallTotal {
		for inFlight > 0 {
			r, ok := <-results
			if !ok {
				break
			}
			if widx := windowStateIndex(windowStates, r.window.Anchor); widx >= 0 && windowStates[widx].inFlight > 0 {
				windowStates[widx].inFlight--
			}
			inFlight--
		}
	}
	if err := ctx.Err(); err != nil && overallDone < overallTotal {
		return err
	}
	for _, st := range states {
		if st.emitted < int64(cfg.SamplesPerPhase) {
			return fmt.Errorf("window: phase %d emitted %d/%d samples", st.phase, st.emitted, cfg.SamplesPerPhase)
		}
	}
	for _, st := range states {
		if opts.Resume && st.existing >= int64(cfg.SamplesPerPhase) {
			continue
		}
		ps := st.stats(cfg)
		ps.StartedAt = st.started.Format(time.RFC3339)
		ps.FinishedAt = time.Now().Format(time.RFC3339)
		ps.ElapsedSec = time.Since(st.started).Seconds()
		fillSpeed(&ps)
		if err := st.writers.flush(); err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(st.dir, "stats.json"), ps); err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(st.dir, "metadata.json"), buildMetadata(cfg, st.phase)); err != nil {
			return err
		}
	}
	if err := writeWindowStats(cfg, windowStates); err != nil {
		return err
	}
	sendWindowProgress(opts.Progress, cfg, windowStates, states, 0, overallDone, overallTotal, opts)
	return writeWindowSummary(cfg, started, states)
}

func (st *windowPhaseState) stats(cfg Config) phaseStats {
	ps := buildStats(cfg, st.phase, cfg.SamplesPerPhase, st.emitted, 0, st.dup, metrics{}, st.values, st.writers.counts)
	ps.DuplicateRate = ratio(st.dup, st.extracted)
	ps.Playout = nil
	ps.SelfPlay = nil
	ps.Window = &windowPhaseStats{
		ExtractedSamples:   st.extracted,
		DuplicateSamples:   st.dup,
		FailedTrajectories: st.metrics.failed,
		EngineTimeouts:     st.metrics.timeouts,
		EngineErrors:       st.metrics.errors,
	}
	return ps
}

func openWindowPhaseStates(cfg Config, phases []int, opts Options) ([]*windowPhaseState, error) {
	var states []*windowPhaseState
	closeStates := func() {
		for _, st := range states {
			if st.writers != nil {
				st.writers.close()
			}
		}
	}
	for i, phase := range phases {
		dir := filepath.Join(cfg.OutputDir, fmt.Sprintf("phase_%02d", phase))
		if opts.Force {
			if err := os.RemoveAll(dir); err != nil {
				closeStates()
				return nil, err
			}
		}
		if !opts.Force && !opts.Resume {
			if err := ensureNoExistingPhaseOutput(dir); err != nil {
				closeStates()
				return nil, err
			}
		}
		hasResumeState := false
		if opts.Resume {
			resumeState, ok, err := loadResumeState(dir)
			if err != nil {
				closeStates()
				return nil, err
			}
			hasResumeState = ok
			if ok && !cfg.SeedSet {
				cfg.Seed = resumeState.Seed
				cfg.ConfigEffectiveHash = effectiveHash(cfg)
			}
			if ok {
				if err := checkResumeMetadata(resumeState, cfg, phase); err != nil {
					if !opts.Repair {
						closeStates()
						return nil, err
					}
					fmt.Fprintf(os.Stderr, "repair: phase_%02d has incompatible run state (%v); rebuilding this phase\n", phase, err)
					if err := os.RemoveAll(dir); err != nil {
						closeStates()
						return nil, err
					}
					hasResumeState = false
				}
			}
			if opts.Repair && !hasResumeState {
				exists, err := phaseOutputExists(dir)
				if err != nil {
					closeStates()
					return nil, err
				}
				if exists {
					fmt.Fprintf(os.Stderr, "repair: phase_%02d has dataset output without run_state.json or metadata.json; rebuilding this phase\n", phase)
					if err := os.RemoveAll(dir); err != nil {
						closeStates()
						return nil, err
					}
				}
			}
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			closeStates()
			return nil, err
		}
		if !opts.Resume || hasResumeState {
			if err := writeJSON(filepath.Join(dir, "run_state.json"), buildRunState(cfg, phase)); err != nil {
				closeStates()
				return nil, err
			}
		}
		w, seen, counts, err := openPhaseWriters(dir, opts.Resume)
		if err != nil {
			if !opts.Resume || !opts.Repair {
				closeStates()
				return nil, err
			}
			fmt.Fprintf(os.Stderr, "repair: phase_%02d has corrupt resume output (%v); rebuilding this phase\n", phase, err)
			if err := os.RemoveAll(dir); err != nil {
				closeStates()
				return nil, err
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				closeStates()
				return nil, err
			}
			hasResumeState = false
			w, seen, counts, err = openPhaseWriters(dir, false)
			if err != nil {
				closeStates()
				return nil, err
			}
			if err := writeJSON(filepath.Join(dir, "run_state.json"), buildRunState(cfg, phase)); err != nil {
				closeStates()
				return nil, err
			}
		}
		existing := counts["train"] + counts["valid"] + counts["test"]
		if opts.Resume && existing == 0 && !hasResumeState {
			if err := writeJSON(filepath.Join(dir, "run_state.json"), buildRunState(cfg, phase)); err != nil {
				w.close()
				closeStates()
				return nil, err
			}
			hasResumeState = true
		}
		if opts.Resume && existing > 0 && !hasResumeState {
			w.close()
			closeStates()
			return nil, fmt.Errorf("phase_%02d: existing records have no run_state.json or metadata.json; use --resume --repair to rebuild only this phase, --force to rebuild all, or delete the phase directory", phase)
		}
		st := &windowPhaseState{
			phase: phase, index: i + 1, dir: dir, writers: w, seen: seen,
			emitted: existing, existing: existing, started: time.Now(), values: newValueAgg(),
		}
		if opts.Resume && existing > 0 {
			resumeStats, err := readStats(filepath.Join(dir, "stats.json"))
			if err == nil {
				st.resumed = true
				st.attempts = resumeStats.Attempts
				st.dup = resumeStats.DuplicatePositions
				if resumeStats.Window != nil {
					st.extracted = resumeStats.Window.ExtractedSamples
				}
				st.metrics = metricsFromStats(resumeStats)
				st.values = valueAggFromStats(resumeStats.Value)
			}
		}
		states = append(states, st)
	}
	return states, nil
}

func firstResumeSeed(cfg Config, phases []int) (int64, bool, error) {
	for _, phase := range phases {
		meta, ok, err := loadResumeState(filepath.Join(cfg.OutputDir, fmt.Sprintf("phase_%02d", phase)))
		if err != nil {
			return 0, false, err
		}
		if ok {
			return meta.Seed, true, nil
		}
	}
	return 0, false, nil
}

func windowWorker(ctx context.Context, cfg Config, worker int, jobs <-chan datasetWindow, attempts *atomic.Int64, out chan<- windowResult) {
	rng := rand.New(rand.NewSource(cfg.Seed + int64(worker+1)*1000003))
	var playEng engine.Engine
	if cfg.Playout.AI != nil {
		eng, err := newEngine(ctx, resolveEngine(cfg, *cfg.Playout.AI))
		if err != nil {
			sendResult(ctx, out, windowResult{err: err, metrics: metrics{errors: 1}})
			return
		}
		defer eng.Close()
		playEng = eng
	}
	selfEng, err := newEngine(ctx, resolveEngine(cfg, cfg.SelfPlay.AI))
	if err != nil {
		sendResult(ctx, out, windowResult{err: err, metrics: metrics{errors: 1}})
		return
	}
	defer selfEng.Close()
	for {
		var window datasetWindow
		var ok bool
		select {
		case window, ok = <-jobs:
			if !ok {
				return
			}
		case <-ctx.Done():
			return
		}
		attempts.Add(1)
		b, met, err := reachPhase(ctx, cfg, window.Anchor, rng, playEng)
		if err != nil {
			if !sendResult(ctx, out, windowResult{window: window, err: err, metrics: met}) {
				return
			}
			continue
		}
		samples, met2, err := finishGameWindow(ctx, selfEng, b.GameText(), window, cfg.SelfPlay.Retries)
		addMetrics(&met, met2)
		if err != nil {
			if !sendResult(ctx, out, windowResult{window: window, err: err, metrics: met}) {
				return
			}
			continue
		}
		if !sendResult(ctx, out, windowResult{window: window, samples: samples, ok: true, metrics: met}) {
			return
		}
	}
}

func finishGameWindow(ctx context.Context, eng engine.Engine, moves string, window datasetWindow, retries int) (map[int]windowSample, metrics, error) {
	var last error
	var total metrics
	for attempt := 0; attempt <= retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, total, err
		}
		met := metrics{selfGames: 1}
		if attempt > 0 {
			if err := eng.Restart(ctx); err != nil {
				met.errors++
				addMetrics(&total, met)
				return nil, total, err
			}
		}
		if err := eng.Reset(ctx); err != nil {
			last = err
			met.errors++
			addMetrics(&total, met)
			continue
		}
		b := board.New()
		if err := replayToEngine(ctx, eng, &b, moves); err != nil {
			last = err
			met.errors++
			addMetrics(&total, met)
			continue
		}
		captured := map[int]board.Board{}
		captureWindowBoard(captured, window, b)
		for {
			if err := ctx.Err(); err != nil {
				addMetrics(&total, met)
				return nil, total, err
			}
			status, _ := b.Status()
			switch status {
			case board.StatusGameOver:
				black, white, _ := b.DiscCounts()
				margin := black - white
				out := map[int]windowSample{}
				for phase, cb := range captured {
					value := margin
					if cb.Player() == board.White {
						value = -value
					}
					out[phase] = windowSample{b: cb, hash: cb.CanonicalHash(), value: int16(value)}
				}
				addMetrics(&total, met)
				return out, total, nil
			case board.StatusPass:
				if err := b.Apply(board.PassMove); err != nil {
					last = err
					met.errors++
					continue
				}
				// Pass is deterministic from board state; any engine desync is surfaced by the next engine operation.
				_ = eng.PushMove(ctx, board.PassMove)
				met.selfPass++
				continue
			}
			mv, err := eng.Go(ctx)
			if err != nil {
				last = err
				met.timeouts++
				break
			}
			if err := b.Apply(mv); err != nil {
				last = err
				met.errors++
				break
			}
			if err := eng.PushMove(ctx, mv); err != nil {
				last = err
				met.errors++
				break
			}
			met.selfMoves++
			captureWindowBoard(captured, window, b)
		}
		addMetrics(&total, met)
	}
	total.failed = 1
	return nil, total, last
}

func captureWindowBoard(out map[int]board.Board, window datasetWindow, b board.Board) {
	phase := phaseOf(b)
	if _, ok := out[phase]; ok {
		return
	}
	for _, target := range window.Phases {
		if target == phase {
			out[phase] = b
			return
		}
	}
}

func windowTargetPhases(cfg Config) []int {
	if !cfg.Window.Enabled {
		return cfg.Phases
	}
	var out []int
	for _, phase := range cfg.Phases {
		if windowIncludesPhase(cfg.Window, phase) {
			out = append(out, phase)
		}
	}
	return out
}

func buildWindows(phases []int, size int) []datasetWindow {
	if len(phases) == 0 || size <= 0 {
		return nil
	}
	sorted := append([]int(nil), phases...)
	sort.Ints(sorted)
	anchors := deriveWindowAnchors(sorted, size)
	var out []datasetWindow
	for _, anchor := range anchors {
		w := datasetWindow{Anchor: anchor}
		for _, phase := range sorted {
			if phase >= anchor && phase < anchor+size {
				w.Phases = append(w.Phases, phase)
			}
		}
		if len(w.Phases) > 0 {
			out = append(out, w)
		}
	}
	return out
}

func buildWindowRunStates(windows []datasetWindow) []*windowRunState {
	out := make([]*windowRunState, 0, len(windows))
	now := time.Now()
	for i, w := range windows {
		out = append(out, &windowRunState{window: w, index: i + 1, started: now})
	}
	return out
}

func nextRunnableWindow(windows []*windowRunState, phases []*windowPhaseState, target int, active *int) int {
	if active == nil {
		idx := 0
		active = &idx
	}
	for *active < len(windows) {
		if windowRemaining(windows[*active].window, phases, target) > 0 {
			return *active
		}
		(*active)++
	}
	return -1
}

func windowRemaining(window datasetWindow, phases []*windowPhaseState, target int) int64 {
	var remaining int64
	for _, phase := range window.Phases {
		st := findPhaseState(phases, phase)
		if st == nil {
			continue
		}
		need := int64(target) - min64(st.emitted, int64(target))
		if need > 0 {
			remaining += need
		}
	}
	return remaining
}

func findPhaseState(states []*windowPhaseState, phase int) *windowPhaseState {
	for _, st := range states {
		if st.phase == phase {
			return st
		}
	}
	return nil
}

func windowStateIndex(windows []*windowRunState, anchor int) int {
	for i, w := range windows {
		if w.window.Anchor == anchor {
			return i
		}
	}
	return -1
}

func deriveWindowAnchors(phases []int, size int) []int {
	if len(phases) == 0 || size <= 0 {
		return nil
	}
	sorted := append([]int(nil), phases...)
	sort.Ints(sorted)
	minPhase := sorted[0]
	maxPhase := sorted[len(sorted)-1]
	var anchors []int
	for anchor := minPhase; anchor <= maxPhase; anchor += size {
		anchors = append(anchors, anchor)
	}
	return anchors
}

func windowIncludesPhase(window WindowConfig, phase int) bool {
	if !window.Enabled {
		return true
	}
	for _, anchor := range window.Anchors {
		if windowAnchorIncludesPhase(window, anchor, phase) {
			return true
		}
	}
	return false
}

func windowAnchorIncludesPhase(window WindowConfig, anchor, phase int) bool {
	return phase >= anchor && phase < anchor+window.Size
}

func phasesOutsideWindow(phases []int, window WindowConfig) []int {
	var out []int
	for _, phase := range phases {
		if !windowIncludesPhase(window, phase) {
			out = append(out, phase)
		}
	}
	return out
}

func formatPhaseList(phases []int) string {
	if len(phases) == 0 {
		return ""
	}
	var parts []string
	start := phases[0]
	prev := phases[0]
	flush := func() {
		if start == prev {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, fmt.Sprintf("%d..%d", start, prev))
		}
	}
	for _, phase := range phases[1:] {
		if phase == prev+1 {
			prev = phase
			continue
		}
		flush()
		start = phase
		prev = phase
	}
	flush()
	return strings.Join(parts, ",")
}

func writeWindowSummary(cfg Config, started time.Time, states []*windowPhaseState) error {
	splits := map[string]int64{"train": 0, "valid": 0, "test": 0}
	var total int64
	var phases []int
	var phaseStats []phaseSummary
	for _, st := range states {
		total += st.emitted
		phases = append(phases, st.phase)
		phaseStats = append(phaseStats, phaseSummary{
			Phase:              st.phase,
			EmittedSamples:     int(st.emitted),
			FailedAttempts:     0,
			DuplicatePositions: st.dup,
			DuplicateRate:      ratio(st.dup, st.extracted),
		})
		for k, v := range st.writers.counts {
			splits[k] += v
		}
	}
	sum := summary{
		Schema: "rdg_summary_v1", Mode: cfg.Mode, Generation: "window",
		StartedAt: started.Format(time.RFC3339), FinishedAt: time.Now().Format(time.RFC3339),
		ElapsedSec: time.Since(started).Seconds(), Phases: phases,
		TotalSamples: total, Split: splits, PhaseStats: phaseStats,
	}
	return writeJSON(filepath.Join(cfg.OutputDir, "summary.json"), sum)
}

func writeWindowStats(cfg Config, states []*windowRunState) error {
	out := windowStatsFile{
		Schema:      "rdg_window_stats_v1",
		Size:        cfg.Window.Size,
		GeneratedAt: time.Now().Format(time.RFC3339),
	}
	for _, st := range states {
		entry := windowStatsEntry{
			Anchor:             st.window.Anchor,
			Phases:             append([]int(nil), st.window.Phases...),
			InFlight:           st.inFlight,
			Trajectories:       st.trajectories,
			SelfPlayGames:      st.metrics.selfGames,
			SelfPlayMoves:      st.metrics.selfMoves,
			SelfPlayPassCount:  st.metrics.selfPass,
			EngineTimeouts:     st.metrics.timeouts,
			EngineErrors:       st.metrics.errors,
			ExtractedSamples:   st.extracted,
			DuplicateSamples:   st.duplicates,
			FailedTrajectories: st.failed,
			ElapsedSeconds:     time.Since(st.started).Seconds(),
		}
		out.TotalTrajectories += st.trajectories
		out.TotalFailedTrajectories += st.failed
		out.TotalExtractedSamples += st.extracted
		out.TotalDuplicateSamples += st.duplicates
		out.Windows = append(out.Windows, entry)
	}
	return writeJSON(filepath.Join(cfg.OutputDir, "window_stats.json"), out)
}

func sendWindowProgress(ch chan<- runui.Event, cfg Config, windows []*windowRunState, phases []*windowPhaseState, current int, overallDone, overallTotal int64, opts Options) {
	if ch == nil || len(windows) == 0 {
		return
	}
	if current < 0 || current >= len(windows) {
		current = 0
	}
	st := windows[current]
	title := "▶️ rdg dataset"
	if opts.NoEmoji {
		title = "rdg dataset"
	}
	ev := runui.Event{
		Stage: "dataset", Title: title, Mode: cfg.Mode, Generation: "window", Strategy: "active_window",
		NoEmoji: opts.NoEmoji, NoColor: opts.NoColor, Output: cfg.OutputDir,
		Done: overallDone, Total: overallTotal, OverallDone: overallDone, OverallTotal: overallTotal,
		Workers: cfg.SelfPlay.Workers,
		Window: runui.WindowEvent{
			Enabled:            true,
			Anchor:             st.window.Anchor,
			Start:              st.window.Phases[0],
			End:                st.window.Phases[len(st.window.Phases)-1],
			Index:              st.index,
			Count:              len(windows),
			Trajectories:       st.trajectories,
			InFlight:           int64(st.inFlight),
			ActiveWorkers:      totalWindowInFlight(windows),
			WorkerCapacity:     windowWorkerCapacity(windows, phases, cfg.SamplesPerPhase, cfg.SelfPlay.Workers),
			ExtractedSamples:   st.extracted,
			DuplicateSamples:   st.duplicates,
			FailedTrajectories: st.failed,
			Timeouts:           st.metrics.timeouts,
			Errors:             st.metrics.errors,
		},
	}
	for _, phase := range st.window.Phases {
		pst := findPhaseState(phases, phase)
		if pst == nil {
			continue
		}
		done := min64(pst.emitted, int64(cfg.SamplesPerPhase))
		ev.Window.Done += done
		ev.Window.Total += int64(cfg.SamplesPerPhase)
		ev.Window.Phases = append(ev.Window.Phases, runui.PhaseProgress{
			Phase: phase,
			Done:  done,
			Total: int64(cfg.SamplesPerPhase),
		})
	}
	select {
	case ch <- ev:
	default:
	}
}

func totalWindowInFlight(windows []*windowRunState) int64 {
	var total int64
	for _, st := range windows {
		total += int64(st.inFlight)
	}
	return total
}

func windowWorkerCapacity(windows []*windowRunState, phases []*windowPhaseState, target, workers int) int64 {
	for _, w := range windows {
		if windowRemaining(w.window, phases, target) > 0 {
			return int64(workers)
		}
	}
	return 0
}
