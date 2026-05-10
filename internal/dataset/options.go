package dataset

import "github.com/ikepggthb/reversi-dataset-gen/internal/runui"

type Options struct {
	Force        bool
	Resume       bool
	Repair       bool
	DryRun       bool
	Progress     chan<- runui.Event
	ProgressJSON bool
	NoProgress   bool
	NoEmoji      bool
	NoColor      bool
}

type progressScope struct {
	PhaseIndex   int
	PhaseCount   int
	OverallDone  int64
	OverallTotal int64
}
