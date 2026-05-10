package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikepggthb/reversi-dataset-gen/internal/runui"
)

func TestRotateLogIfNeeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rdg.log")
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(path, strings.Repeat("x", 12))
	write(path+".1", "one")
	write(path+".2", "two")

	if err := rotateLogIfNeeded(path, 10, 2); err != nil {
		t.Fatalf("rotateLogIfNeeded: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("current log should be rotated away, stat err=%v", err)
	}
	if got := mustRead(t, path+".1"); got != strings.Repeat("x", 12) {
		t.Fatalf("log.1 = %q", got)
	}
	if got := mustRead(t, path+".2"); got != "one" {
		t.Fatalf("log.2 = %q", got)
	}
}

func TestRotateLogDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rdg.log")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rotateLogIfNeeded(path, 1, 0); err != nil {
		t.Fatalf("rotateLogIfNeeded: %v", err)
	}
	if got := mustRead(t, path); got != "hello" {
		t.Fatalf("log changed: %q", got)
	}
}

func TestAddFileLogSinkClosesCleanly(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	primary := make(chan runui.Event, 4)
	primaryDone := make(chan struct{})
	go func() {
		for range primary {
		}
		close(primaryDone)
	}()
	in, done, err := addFileLogSink(ctx, primary, primaryDone, filepath.Join(t.TempDir(), "rdg.log"), 0, 0, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	in <- runui.Event{Stage: "test"}
	close(in)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("addFileLogSink did not finish within 2s")
	}
}

func TestRunRejectsConflictingProgressFlags(t *testing.T) {
	if code := run([]string{"--no-progress", "--json-progress"}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
