package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikepggthb/reversi-dataset-gen/internal/config"
)

func TestTargetForKind(t *testing.T) {
	cases := []struct {
		kind config.EngineKind
		want string
	}{
		{config.EngineEdax, "setup-edax"},
		{config.EngineEgaroucid, "setup-egaroucid"},
	}
	for _, c := range cases {
		got, err := targetForKind(c.kind)
		if err != nil {
			t.Fatalf("targetForKind(%q): %v", c.kind, err)
		}
		if got != c.want {
			t.Fatalf("targetForKind(%q) = %q, want %q", c.kind, got, c.want)
		}
	}
}

func TestTargetForKindRejectsUnknown(t *testing.T) {
	if _, err := targetForKind(config.EngineKind("unknown")); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMissingRequirements(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "engine")
	eval := filepath.Join(dir, "eval.dat")
	eng := config.Engine{
		Kind:    config.EngineEdax,
		Binary:  bin,
		Options: map[string]any{"eval_file": eval},
	}

	missing, err := missingRequirements(eng)
	if err != nil {
		t.Fatalf("missingRequirements: %v", err)
	}
	if len(missing) != 2 {
		t.Fatalf("missing len = %d, want 2: %v", len(missing), missing)
	}

	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatalf("write bin: %v", err)
	}
	if err := os.WriteFile(eval, []byte("eval"), 0o644); err != nil {
		t.Fatalf("write eval: %v", err)
	}
	missing, err = missingRequirements(eng)
	if err != nil {
		t.Fatalf("missingRequirements: %v", err)
	}
	if len(missing) != 1 {
		t.Fatalf("missing len = %d, want 1: %v", len(missing), missing)
	}

	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatalf("chmod bin: %v", err)
	}
	missing, err = missingRequirements(eng)
	if err != nil {
		t.Fatalf("missingRequirements: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none", missing)
	}
}

func TestConfirm(t *testing.T) {
	ok, err := confirm(Options{Stdin: strings.NewReader("y\n")}, "Run?")
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !ok {
		t.Fatal("confirm should accept y")
	}
	ok, err = confirm(Options{Stdin: strings.NewReader("\n")}, "Run?")
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if ok {
		t.Fatal("confirm should reject empty answer")
	}
}
