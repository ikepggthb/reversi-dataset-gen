package dataset

import (
	"github.com/ikepggthb/reversi-dataset-gen/internal/runui"
)

func sendProgress(ch chan<- runui.Event, cfg Config, phase int, total, done int64, st phaseStats, opts Options, scope *progressScope) {
	if ch == nil {
		return
	}
	title := "▶️ rdg dataset"
	if opts.NoEmoji {
		title = "rdg dataset"
	}
	overallDone := done
	overallTotal := total
	phaseIndex := 1
	phaseCount := 1
	if scope != nil {
		overallDone = scope.OverallDone + done
		overallTotal = scope.OverallTotal
		phaseIndex = scope.PhaseIndex
		phaseCount = scope.PhaseCount
	}
	ev := runui.Event{
		Stage: "dataset", Title: title, Mode: cfg.Mode, Phase: phase, NoEmoji: opts.NoEmoji, NoColor: opts.NoColor,
		Output: cfg.OutputDir, Done: overallDone, Total: overallTotal,
		OverallDone: overallDone, OverallTotal: overallTotal,
		PhaseDone: done, PhaseTotal: total, PhaseIndex: phaseIndex, PhaseCount: phaseCount,
		Attempts: st.Attempts, Duplicates: st.DuplicatePositions,
		Failures: st.FailedAttempts,
		Train:    st.Split["train"], Valid: st.Split["valid"], Test: st.Split["test"],
		Workers: cfg.SelfPlay.Workers,
	}
	if st.SelfPlay != nil {
		ev.Timeouts = st.SelfPlay.EngineTimeouts
		ev.Errors = st.SelfPlay.EngineErrors
	}
	select {
	case ch <- ev:
	default:
	}
}
