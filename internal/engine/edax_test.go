package engine

import (
	"slices"
	"testing"
	"time"

	"github.com/ikepggthb/reversi-dataset-gen/internal/config"
)

func TestNewEdaxSpec(t *testing.T) {
	cfg := config.Engine{
		Kind:    config.EngineEdax,
		Binary:  "/path/to/lEdax",
		Level:   8,
		Threads: 2,
		Timeout: 30 * time.Second,
		Options: map[string]any{
			"eval_file": "/path/to/eval.dat",
		},
	}
	s, err := newEdaxSpec(cfg)
	if err != nil {
		t.Fatalf("newEdaxSpec: %v", err)
	}
	if s.Name != "edax" {
		t.Errorf("Name = %q", s.Name)
	}
	if !slices.Contains(s.Args, "-nboard") {
		t.Errorf("Args missing -nboard: %v", s.Args)
	}
	if !slices.Contains(s.Args, "/path/to/eval.dat") {
		t.Errorf("Args missing eval file: %v", s.Args)
	}
	if !slices.Contains(s.Args, "8") || !slices.Contains(s.Args, "2") {
		t.Errorf("Args missing level/threads: %v", s.Args)
	}
	if s.StartupAck != "pong 1" {
		t.Errorf("StartupAck = %q", s.StartupAck)
	}
	if s.DoneAck != "status Edax is waiting" {
		t.Errorf("DoneAck = %q", s.DoneAck)
	}
}

func TestNewEdaxSpecDefaultEvalFile(t *testing.T) {
	cfg := config.Engine{
		Kind:    config.EngineEdax,
		Binary:  "/opt/edax/bin/lEdax",
		Level:   1,
		Timeout: 30 * time.Second,
	}
	s, err := newEdaxSpec(cfg)
	if err != nil {
		t.Fatalf("newEdaxSpec: %v", err)
	}
	want := "/opt/edax/bin/data/eval.dat"
	if !slices.Contains(s.Args, want) {
		t.Errorf("default eval file = %v not found in args %v", want, s.Args)
	}
}
