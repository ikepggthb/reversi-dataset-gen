package dataset

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikepggthb/reversi-dataset-gen/internal/board"
	"github.com/ikepggthb/reversi-dataset-gen/internal/config"
	"github.com/ikepggthb/reversi-dataset-gen/internal/engine"
	"github.com/ikepggthb/reversi-dataset-gen/internal/enginetest"
)

func TestParsePhases(t *testing.T) {
	cases := map[string][]int{
		"11":       {11},
		"11,12,20": {11, 12, 20},
		"1..3":     {1, 2, 3},
	}
	for in, want := range cases {
		got, err := parsePhases(in)
		if err != nil {
			t.Fatalf("parsePhases(%q): %v", in, err)
		}
		if len(got) != len(want) {
			t.Fatalf("parsePhases(%q) len = %d, want %d", in, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("parsePhases(%q)[%d] = %d, want %d", in, i, got[i], want[i])
			}
		}
	}
}

func TestWindowTargetPhasesUsesIntersection(t *testing.T) {
	cfg := Config{
		Phases: []int{12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25},
		Window: WindowConfig{
			Enabled: true,
			Anchors: []int{12, 22},
			Size:    10,
		},
	}
	got := windowTargetPhases(cfg)
	want := []int{12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %d, want %d", i, got[i], want[i])
		}
	}
	cfg.Phases = []int{8, 9, 10, 11, 19, 20, 29, 30}
	cfg.Window.Anchors = deriveWindowAnchors(cfg.Phases, cfg.Window.Size)
	got = windowTargetPhases(cfg)
	want = []int{8, 9, 10, 11, 19, 20, 29, 30}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestDeriveWindowAnchorsFromPhasesAndSize(t *testing.T) {
	got := deriveWindowAnchors([]int{60, 10, 11, 20}, 10)
	want := []int{10, 20, 30, 40, 50, 60}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestCaptureWindowBoardUsesAnchorRangeOnly(t *testing.T) {
	window10 := datasetWindow{Anchor: 10, Phases: []int{10, 11, 12, 13, 14, 15, 16, 17, 18, 19}}
	window20 := datasetWindow{Anchor: 20, Phases: []int{20, 21, 22, 23, 24, 25, 26, 27, 28, 29}}
	captured := map[int]board.Board{}
	b := board.New()
	for phaseOf(b) < 20 {
		status, legal := b.Status()
		switch status {
		case board.StatusPass:
			if err := b.Apply(board.PassMove); err != nil {
				t.Fatal(err)
			}
		case board.StatusPlay:
			if err := b.Apply(board.Move(legal[0])); err != nil {
				t.Fatal(err)
			}
		case board.StatusGameOver:
			t.Fatal("unexpected game over")
		}
	}
	captureWindowBoard(captured, window10, b)
	if _, ok := captured[20]; ok {
		t.Fatal("anchor 10 should not capture phase 20")
	}
	captureWindowBoard(captured, window20, b)
	if _, ok := captured[20]; !ok {
		t.Fatal("anchor 20 should capture phase 20")
	}
}

func TestFinishGameWindowCapturesMultiplePhasesWithMockEngine(t *testing.T) {
	eng := enginetest.New()
	samples, met, err := finishGameWindow(t.Context(), eng, "f5", datasetWindow{Anchor: 1, Phases: []int{1, 2, 3}}, 0)
	if err != nil {
		t.Fatalf("finishGameWindow: %v", err)
	}
	for _, phase := range []int{1, 2, 3} {
		s, ok := samples[phase]
		if !ok {
			t.Fatalf("missing phase %d sample; got phases %v", phase, samples)
		}
		if phaseOf(s.b) != phase {
			t.Fatalf("sample phase = %d, want %d", phaseOf(s.b), phase)
		}
		if s.hash == "" {
			t.Fatalf("phase %d hash is empty", phase)
		}
	}
	if _, ok := samples[4]; ok {
		t.Fatal("phase 4 should be outside anchor 1 size 3")
	}
	if met.selfGames != 1 || met.selfMoves == 0 {
		t.Fatalf("unexpected metrics: %+v", met)
	}
	if eng.GoCalls == 0 || eng.PushCalls == 0 {
		t.Fatalf("mock engine was not used: go=%d push=%d", eng.GoCalls, eng.PushCalls)
	}
}

func TestFinishGameReturnsPromptlyWhenGoContextIsCanceled(t *testing.T) {
	entered := make(chan struct{})
	eng := enginetest.New()
	eng.GoWithContext = func(ctx context.Context, _ board.Board) (board.Move, error) {
		close(entered)
		<-ctx.Done()
		return board.PassMove, ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, _, err := finishGame(ctx, eng, "", board.Black, 0)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("engine Go was not reached")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("finishGame returned nil error after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("finishGame did not return promptly after context cancellation")
	}
}

func TestActiveWindowSchedulerAllowsMultipleInFlightPerWindow(t *testing.T) {
	windows := buildWindowRunStates([]datasetWindow{
		{Anchor: 10, Phases: []int{10, 11}},
		{Anchor: 20, Phases: []int{20, 21}},
	})
	states := []*windowPhaseState{
		{phase: 10, emitted: 0},
		{phase: 11, emitted: 100},
		{phase: 20, emitted: 0},
		{phase: 21, emitted: 100},
	}
	if got := windowWorkerCapacity(windows, states, 100, 19); got != 19 {
		t.Fatalf("capacity = %d, want 19", got)
	}
	active := 0
	idx := nextRunnableWindow(windows, states, 100, &active)
	if idx != 0 {
		t.Fatalf("first runnable = %d, want 0", idx)
	}
	windows[idx].inFlight = 1
	idx = nextRunnableWindow(windows, states, 100, &active)
	if idx != 0 {
		t.Fatalf("active window should remain runnable while in-flight = %d, want 0", idx)
	}
	windows[idx].inFlight = 2
	idx = nextRunnableWindow(windows, states, 100, &active)
	if idx != 0 {
		t.Fatalf("same window should remain runnable while in-flight = %d, want 0", idx)
	}
	if got := totalWindowInFlight(windows); got != 2 {
		t.Fatalf("in-flight = %d, want 2", got)
	}
}

func TestActiveWindowSchedulerAdvancesOnlyAfterCompletion(t *testing.T) {
	windows := buildWindowRunStates([]datasetWindow{
		{Anchor: 10, Phases: []int{10, 11}},
		{Anchor: 20, Phases: []int{20, 21}},
		{Anchor: 30, Phases: []int{30, 31}},
	})
	states := []*windowPhaseState{
		{phase: 10, emitted: 50},
		{phase: 11, emitted: 100},
		{phase: 20, emitted: 10},
		{phase: 21, emitted: 100},
		{phase: 30, emitted: 50},
		{phase: 31, emitted: 100},
	}
	if got := windowRemaining(windows[0].window, states, 100); got != 50 {
		t.Fatalf("remaining window 10 = %d, want 50", got)
	}
	if got := windowRemaining(windows[1].window, states, 100); got != 90 {
		t.Fatalf("remaining window 20 = %d, want 90", got)
	}
	active := 0
	if idx := nextRunnableWindow(windows, states, 100, &active); idx != 0 {
		t.Fatalf("active window should ignore larger remaining later window = %d, want 0", idx)
	}
	states[0].emitted = 100
	if idx := nextRunnableWindow(windows, states, 100, &active); idx != 1 {
		t.Fatalf("after active completion should advance to next window = %d, want 1", idx)
	}
	if active != 1 {
		t.Fatalf("active index = %d, want 1", active)
	}
	for _, st := range states {
		st.emitted = 100
	}
	if idx := nextRunnableWindow(windows, states, 100, &active); idx != -1 {
		t.Fatalf("completed windows should not be runnable = %d, want -1", idx)
	}
	if got := windowWorkerCapacity(windows, states, 100, 19); got != 0 {
		t.Fatalf("completed capacity = %d, want 0", got)
	}
}

func TestEffectiveHashIgnoresSamplesPerPhase(t *testing.T) {
	cfg := Config{
		Mode:            ModeNormal,
		Phases:          []int{11, 12},
		SamplesPerPhase: 100,
		Seed:            1,
		Split:           SplitConfig{Train: 0.98, Valid: 0.01, Test: 0.01},
		Playout:         PlayoutConfig{AIProbability: 0, Dedupe: "canonical_board", MaxAttemptsMultiplier: 100},
		SelfPlay:        SelfPlayConfig{AI: AIConfig{Name: "edax", Level: 1, Threads: 1}, Workers: 2, Retries: 3},
		AppConfigSHA256: "app",
	}
	a := effectiveHash(cfg)
	cfg.SamplesPerPhase = 1000
	b := effectiveHash(cfg)
	if a != b {
		t.Fatalf("effective hash should ignore samples_per_phase: %s != %s", a, b)
	}
	cfg.SelfPlay.AI.Level = 2
	c := effectiveHash(cfg)
	if c == a {
		t.Fatal("effective hash should change when generation content changes")
	}
	cfg.Window = WindowConfig{Enabled: true, Anchors: deriveWindowAnchors([]int{10}, 10), Size: 10}
	d := effectiveHash(cfg)
	if d == c {
		t.Fatal("effective hash should change when window config changes")
	}
}

func TestRDRecordCountAndHashRecovery(t *testing.T) {
	dir := t.TempDir()
	rd := filepath.Join(dir, "train.rd")
	var rec [RecordSize]byte
	binary.LittleEndian.PutUint64(rec[0:8], 1)
	binary.LittleEndian.PutUint64(rec[8:16], 2)
	v := int16(-4)
	binary.LittleEndian.PutUint16(rec[16:18], uint16(v))
	if err := os.WriteFile(rd, append([]byte(MagicBitboard), rec[:]...), 0o644); err != nil {
		t.Fatal(err)
	}
	count, err := recordCount(rd)
	if err != nil {
		t.Fatalf("recordCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("recordCount = %d, want 1", count)
	}
	seen := map[string]bool{}
	if err := readRecordHashes(rd, seen); err != nil {
		t.Fatalf("readRecordHashes: %v", err)
	}
	if len(seen) != 1 {
		t.Fatalf("seen hashes = %d, want 1", len(seen))
	}
}

func TestRecordCountTreatsEmptyFileAsMissing(t *testing.T) {
	rd := filepath.Join(t.TempDir(), "train.rd")
	if err := os.WriteFile(rd, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := recordCount(rd); !os.IsNotExist(err) {
		t.Fatalf("recordCount error = %v, want os.IsNotExist", err)
	}
}

func TestResumeUsesRunStateWhenMetadataIsMissing(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Mode:            ModeNormal,
		OutputDir:       filepath.Join(dir, "datasets"),
		Phases:          []int{1},
		SamplesPerPhase: 1,
		Seed:            7,
		SeedSet:         true,
		Split:           SplitConfig{Train: 1},
		Playout:         PlayoutConfig{Dedupe: "canonical_board", MaxAttemptsMultiplier: 10},
		SelfPlay:        SelfPlayConfig{AI: AIConfig{Name: "edax", Level: 1, Threads: 1}, Workers: 1, Retries: 1},
		AppConfigSHA256: "app",
		ConfigSHA256:    "config",
		EngineConfig:    &config.Config{},
	}
	cfg.ConfigEffectiveHash = effectiveHash(cfg)
	phaseDir := filepath.Join(cfg.OutputDir, "phase_01")
	if err := os.MkdirAll(phaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(phaseDir, "run_state.json"), buildRunState(cfg, 1)); err != nil {
		t.Fatal(err)
	}
	var rec [RecordSize]byte
	binary.LittleEndian.PutUint64(rec[0:8], 1)
	if err := os.WriteFile(filepath.Join(phaseDir, "train.rd"), append([]byte(MagicBitboard), rec[:]...), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Run(t.Context(), cfg, Options{Resume: true}); err != nil {
		t.Fatalf("Run resume: %v", err)
	}
	if _, err := os.Stat(filepath.Join(phaseDir, "metadata.json")); err != nil {
		t.Fatalf("metadata.json after resume: %v", err)
	}
	if _, err := os.Stat(filepath.Join(phaseDir, "stats.json")); err != nil {
		t.Fatalf("stats.json after resume: %v", err)
	}
	count, err := recordCount(filepath.Join(phaseDir, "train.rd"))
	if err != nil {
		t.Fatalf("recordCount after resume: %v", err)
	}
	if count != 1 {
		t.Fatalf("record count after resume = %d, want 1", count)
	}
}

func TestEnsureNoExistingPhaseOutput(t *testing.T) {
	dir := t.TempDir()
	if err := ensureNoExistingPhaseOutput(dir); err != nil {
		t.Fatalf("empty dir should be allowed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureNoExistingPhaseOutput(dir); err != nil {
		t.Fatalf("unrelated file should be allowed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "train.rd"), []byte(MagicBitboard), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ensureNoExistingPhaseOutput(dir)
	if err == nil || !strings.Contains(err.Error(), "--resume --repair") {
		t.Fatalf("expected existing output error, got %v", err)
	}
}

func TestAllPositionsEnumerationAddsDuplicateRate(t *testing.T) {
	positions, enum := enumeratePhase(2)
	enum.DuplicateRate = ratio(int64(enum.Duplicates), int64(enum.Leaf))
	if len(positions) == 0 {
		t.Fatal("expected positions")
	}
	if enum.DuplicateRate <= 0 {
		t.Fatalf("duplicate_rate missing or wrong value: %#v", enum.DuplicateRate)
	}
}

func TestMetricsFromStatsReadsWindowMetrics(t *testing.T) {
	got := metricsFromStats(phaseStats{
		FailedAttempts: 99,
		Window: &windowPhaseStats{
			FailedTrajectories: 3,
			EngineTimeouts:     4,
			EngineErrors:       5,
		},
	})
	if got.failed != 3 || got.timeouts != 4 || got.errors != 5 {
		t.Fatalf("metrics = %+v, want failed=3 timeouts=4 errors=5", got)
	}
}

func TestBitboardSideToMoveRecordPerspective(t *testing.T) {
	b := board.New()
	if err := b.Apply(board.Move(mustSquare(t, "f5"))); err != nil {
		t.Fatal(err)
	}
	own, opp := b.BitboardsSideToMove()
	if own == 0 || opp == 0 {
		t.Fatalf("own/opponent bitboards should both be non-zero: %064b %064b", own, opp)
	}
	if own&opp != 0 {
		t.Fatalf("own and opponent overlap: %064b", own&opp)
	}
}

func TestLoadConfigAllowsNoPlayoutAIWhenProbabilityZero(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "app.toml")
	if err := os.WriteFile(app, []byte(`
[engines.edax]
kind = "edax"
binary = "edax"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
mode = "normal"
output_dir = "datasets"
phases = "11"
samples_per_phase = 1

[playout]
ai_probability = 0

[self_play.ai]
name = "edax"
level = 1
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(cfgPath, app)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Playout.AI != nil {
		t.Fatalf("playout AI should be nil when ai_probability is 0")
	}
}

func TestLoadConfigRequiresPlayoutAIWhenProbabilityPositive(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "app.toml")
	if err := os.WriteFile(app, []byte(`
[engines.edax]
kind = "edax"
binary = "edax"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
mode = "normal"
output_dir = "datasets"
phases = "11"
samples_per_phase = 1

[playout]
ai_probability = 0.5

[self_play.ai]
name = "edax"
level = 1
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(cfgPath, app)
	if err == nil || !strings.Contains(err.Error(), "playout.ai.name") {
		t.Fatalf("expected playout.ai.name error, got %v", err)
	}
}

func TestLoadConfigParsesWindow(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "app.toml")
	if err := os.WriteFile(app, []byte(`
[engines.edax]
kind = "edax"
binary = "edax"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
mode = "normal"
output_dir = "datasets"
phases = "10..29"
samples_per_phase = 1

[playout]
ai_probability = 0

[self_play.ai]
name = "edax"
level = 1

[window]
enabled = true
size = 10
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(cfgPath, app)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.Window.Enabled || cfg.Window.Size != 10 || len(cfg.Window.Anchors) != 2 {
		t.Fatalf("window config not parsed: %+v", cfg.Window)
	}
	if got := len(windowTargetPhases(cfg)); got != 20 {
		t.Fatalf("window target phases = %d, want 20", got)
	}
}

func TestRunWindowModeWritesPhaseDatasetsWithMockEngine(t *testing.T) {
	oldNewEngine := newEngine
	var created []*enginetest.Mock
	newEngine = func(ctx context.Context, cfg config.Engine) (engine.Engine, error) {
		eng := enginetest.New()
		created = append(created, eng)
		return eng, nil
	}
	t.Cleanup(func() { newEngine = oldNewEngine })

	dir := t.TempDir()
	app := filepath.Join(dir, "app.toml")
	if err := os.WriteFile(app, []byte(`
[engines.edax]
kind = "edax"
binary = "edax"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
mode = "normal"
output_dir = "datasets"
phases = "1..3"
samples_per_phase = 1
seed = 1

[split]
train = 1
valid = 0
test = 0

[playout]
ai_probability = 0
dedupe = "canonical_board"
max_attempts_multiplier = 10

[self_play]
workers = 1
retries = 1

[self_play.ai]
name = "edax"
level = 1

[window]
enabled = true
size = 3
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(cfgPath, app)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if err := Run(t.Context(), cfg, Options{Force: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("created engines = %d, want 1", len(created))
	}
	if created[0].ResetCalls != 1 {
		t.Fatalf("self-play resets = %d, want 1; phase 2/3 should be extracted from the same self-play", created[0].ResetCalls)
	}
	for _, phase := range []int{1, 2, 3} {
		rd := filepath.Join(cfg.OutputDir, "phase_"+twoDigits(phase), "train.rd")
		count, err := recordCount(rd)
		if err != nil {
			t.Fatalf("phase %d recordCount: %v", phase, err)
		}
		if count != 1 {
			t.Fatalf("phase %d records = %d, want 1", phase, count)
		}
		if _, err := os.Stat(filepath.Join(cfg.OutputDir, "phase_"+twoDigits(phase), "hashes.jsonl")); !os.IsNotExist(err) {
			t.Fatalf("phase %d hashes.jsonl exists or stat failed: %v", phase, err)
		}
		if _, err := os.Stat(filepath.Join(cfg.OutputDir, "phase_"+twoDigits(phase), "run_state.json")); err != nil {
			t.Fatalf("phase %d run_state.json: %v", phase, err)
		}
		statsRaw, err := os.ReadFile(filepath.Join(cfg.OutputDir, "phase_"+twoDigits(phase), "stats.json"))
		if err != nil {
			t.Fatalf("phase %d stats: %v", phase, err)
		}
		var stats map[string]any
		if err := json.Unmarshal(statsRaw, &stats); err != nil {
			t.Fatalf("phase %d stats json: %v", phase, err)
		}
		for _, forbidden := range []string{"self_play", "playout", "attempts", "failed_attempts"} {
			if _, ok := stats[forbidden]; ok {
				t.Fatalf("phase %d stats should not contain %q: %s", phase, forbidden, statsRaw)
			}
		}
		window, ok := stats["window"].(map[string]any)
		if !ok || window["extracted_samples"].(float64) < 1 {
			t.Fatalf("phase %d stats should contain window extracted samples: %s", phase, statsRaw)
		}
	}
	windowRaw, err := os.ReadFile(filepath.Join(cfg.OutputDir, "window_stats.json"))
	if err != nil {
		t.Fatalf("window_stats.json: %v", err)
	}
	var windowStats windowStatsFile
	if err := json.Unmarshal(windowRaw, &windowStats); err != nil {
		t.Fatalf("window stats json: %v", err)
	}
	if windowStats.TotalTrajectories != 1 || len(windowStats.Windows) != 1 {
		t.Fatalf("unexpected window stats: %+v", windowStats)
	}
	if windowStats.TotalExtractedSamples != 3 {
		t.Fatalf("extracted samples = %d, want 3", windowStats.TotalExtractedSamples)
	}
}

func TestLoadConfigRejectsWindowAnchors(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "app.toml")
	if err := os.WriteFile(app, []byte(`
[engines.edax]
kind = "edax"
binary = "edax"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
mode = "normal"
output_dir = "datasets"
phases = "30"
samples_per_phase = 1

[playout]
ai_probability = 0

[self_play.ai]
name = "edax"
level = 1

[window]
enabled = true
anchors = "10"
size = 10
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(cfgPath, app)
	if err == nil || !strings.Contains(err.Error(), "window.anchors is no longer supported") {
		t.Fatalf("expected window anchors error, got %v", err)
	}
}

func TestLoadConfigAutoGeneratesWindowAnchors(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "app.toml")
	if err := os.WriteFile(app, []byte(`
[engines.edax]
kind = "edax"
binary = "edax"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
mode = "normal"
output_dir = "datasets"
phases = "11..60"
samples_per_phase = 1

[playout]
ai_probability = 0

[self_play.ai]
name = "edax"
level = 1

[window]
enabled = true
size = 10
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(cfgPath, app)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	want := []int{11, 21, 31, 41, 51}
	if len(cfg.Window.Anchors) != len(want) {
		t.Fatalf("anchors = %v, want %v", cfg.Window.Anchors, want)
	}
	for i := range want {
		if cfg.Window.Anchors[i] != want[i] {
			t.Fatalf("anchors = %v, want %v", cfg.Window.Anchors, want)
		}
	}
}

func TestLoadConfigRejectsWindowInAllPositions(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "app.toml")
	if err := os.WriteFile(app, []byte(`
[engines.edax]
kind = "edax"
binary = "edax"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
mode = "all_positions"
output_dir = "datasets"
phases = "1"
samples_per_phase = 0

[playout]
ai_probability = 0

[self_play.ai]
name = "edax"
level = 1

[window]
enabled = true
size = 1
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(cfgPath, app)
	if err == nil || !strings.Contains(err.Error(), "window is only supported in normal mode") {
		t.Fatalf("expected all_positions window error, got %v", err)
	}
}

func mustSquare(t *testing.T, s string) board.Square {
	t.Helper()
	sq, err := board.ParseSquare(s)
	if err != nil {
		t.Fatal(err)
	}
	return sq
}

func twoDigits(n int) string {
	return fmt.Sprintf("%02d", n)
}
