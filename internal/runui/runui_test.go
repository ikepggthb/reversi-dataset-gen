package runui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestFormatDatasetFrame(t *testing.T) {
	out := Format(Event{
		Stage: "dataset", Title: "▶️ rdg dataset", Mode: "normal", Phase: 11,
		Output: "datasets", OverallDone: 1250, OverallTotal: 5000,
		PhaseDone: 250, PhaseTotal: 1000, PhaseIndex: 2, PhaseCount: 5,
		Attempts: 400, Duplicates: 150,
		Failures: 2, Timeouts: 1, Errors: 1, Workers: 4, Train: 240, Valid: 5, Test: 5, NoColor: true,
	}, time.Now().Add(-time.Minute), "2026-05-05 12:00:00 +0900")
	for _, want := range []string{"╭─ ▶️ rdg dataset", "▶️ normal", "Overall", "progress 1,250 / 5,000", "Phase", "progress 250 / 1,000", "attempts", "duplicate", "failures", "datasets"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestFrameKeepsBordersAlignedWithEmojiAndWideText(t *testing.T) {
	out := frame("▶️ rdg dataset", []string{
		"Mode     ▶️ normal",
		"Detail   日本語の長いメッセージとemoji ✅✅✅",
	}, true, "")
	for _, line := range strings.Split(out, "\n") {
		if got := displayWidth(line); got != 78 {
			t.Fatalf("line width = %d, want 78: %q\n%s", got, line, out)
		}
	}
}

func TestFormatNoColorAndNoEmoji(t *testing.T) {
	out := Format(Event{
		Stage: "dataset", Title: "rdg dataset", Mode: "all_positions", Phase: 2,
		OverallDone: 10, OverallTotal: 20, PhaseDone: 10, PhaseTotal: 20, NoColor: true, NoEmoji: true,
	}, time.Now().Add(-time.Minute), "2026-05-05 12:00:00 +0900")
	if strings.Contains(out, "\x1b[") || strings.Contains(out, "▶️") {
		t.Fatalf("unexpected color or emoji:\n%s", out)
	}
}

func TestFormatLine(t *testing.T) {
	out := FormatLine(Event{Mode: "normal", Phase: 11, PhaseIndex: 1, PhaseCount: 3, OverallDone: 12, OverallTotal: 300, PhaseDone: 12, PhaseTotal: 100, Duplicates: 3, Failures: 1}, time.Minute)
	for _, want := range []string{"phase=01/03", "phase_samples=12/100", "overall=12/300", "duplicates=3", "failures=1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("line missing %q: %s", want, out)
		}
	}
}

func TestFormatWindowDatasetFrame(t *testing.T) {
	out := Format(Event{
		Stage: "dataset", Title: "▶️ rdg dataset", Mode: "normal", Generation: "window", Strategy: "active_window",
		Output: "datasets", OverallDone: 1235, OverallTotal: 5100, Workers: 19, NoColor: true,
		Window: WindowEvent{
			Enabled: true, Anchor: 10, Start: 10, End: 19, Index: 1, Count: 6,
			Done: 742, Total: 1000, Trajectories: 128, InFlight: 1, ActiveWorkers: 6, WorkerCapacity: 6,
			ExtractedSamples: 1184, DuplicateSamples: 93, FailedTrajectories: 2,
			Phases: []PhaseProgress{{Phase: 10, Done: 100, Total: 100}, {Phase: 11, Done: 98, Total: 100}},
		},
	}, time.Now().Add(-time.Minute), "2026-05-05 12:00:00 +0900")
	for _, want := range []string{"generation window", "strategy active_window", "Active Window", "covers   10..19", "Trajectory", "self-play games", "active workers   6 / 19", "Phases", "10  100 / 100"} {
		if !strings.Contains(out, want) {
			t.Fatalf("window output missing %q:\n%s", want, out)
		}
	}
}

func TestFormatWindowLine(t *testing.T) {
	out := FormatLine(Event{
		Mode: "normal", Generation: "window", Strategy: "active_window", OverallDone: 12, OverallTotal: 30, Workers: 8,
		Window: WindowEvent{Enabled: true, Anchor: 10, Start: 10, End: 19, Index: 1, Count: 3, Done: 12, Total: 20, Trajectories: 2, InFlight: 1, ActiveWorkers: 3, WorkerCapacity: 3},
	}, time.Minute)
	for _, want := range []string{"generation=window", "strategy=active_window", "active_window=1/3", "anchor=10", "covers=10..19", "overall=12/30", "trajectories=2", "active_workers=3/8", "in_flight=1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("window line missing %q: %s", want, out)
		}
	}
}

func TestStateResetsPhaseScopedCountersOnPhaseChange(t *testing.T) {
	state := NewState()
	state.Apply(Event{
		Mode: "normal", Phase: 11, PhaseIndex: 1, PhaseCount: 2,
		OverallDone: 1000, OverallTotal: 2000, PhaseDone: 1000, PhaseTotal: 1000,
		Attempts: 1200, Duplicates: 20, Train: 980, Valid: 10, Test: 10,
	})
	state.Apply(Event{
		Mode: "normal", Phase: 12, PhaseIndex: 2, PhaseCount: 2,
		OverallDone: 1000, OverallTotal: 2000, PhaseDone: 0, PhaseTotal: 1000,
	})
	got, _, _, ok := state.snapshot()
	if !ok {
		t.Fatal("state has no snapshot")
	}
	if got.PhaseDone != 0 || got.Attempts != 0 || got.Train != 0 {
		t.Fatalf("phase scoped counters were not reset: %+v", got)
	}
	if got.OverallDone != 1000 || got.OverallTotal != 2000 {
		t.Fatalf("overall progress changed unexpectedly: %+v", got)
	}
}

func TestStateWindowUsesCurrentValues(t *testing.T) {
	state := NewState()
	state.Apply(Event{
		Mode: "normal", Generation: "window", OverallDone: 20, OverallTotal: 100,
		Window: WindowEvent{Enabled: true, Anchor: 10, Start: 10, End: 19, Index: 1, Count: 2, Done: 20, Total: 50, InFlight: 1},
	})
	state.Apply(Event{
		Mode: "normal", Generation: "window", OverallDone: 19, OverallTotal: 100,
		Window: WindowEvent{Enabled: true, Anchor: 10, Start: 10, End: 19, Index: 1, Count: 2, Done: 19, Total: 50, InFlight: 0},
	})
	got, _, _, ok := state.snapshot()
	if !ok {
		t.Fatal("state has no snapshot")
	}
	if got.OverallDone != 19 || got.Window.Done != 19 || got.Window.InFlight != 0 {
		t.Fatalf("window state should adopt current values: %+v", got)
	}
}

func TestRendererNonTTYAppendsSnapshots(t *testing.T) {
	var buf bytes.Buffer
	r := newRenderer(&buf)
	r.Render("first")
	r.Render("second")
	got := buf.String()
	if got != "first\nsecond\n" {
		t.Fatalf("non-tty render = %q", got)
	}
}
