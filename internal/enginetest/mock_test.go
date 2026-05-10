package enginetest

import (
	"context"
	"testing"

	"github.com/ikepggthb/reversi-dataset-gen/internal/board"
)

func TestMockFirstLegalEngineFlow(t *testing.T) {
	ctx := context.Background()
	eng := New()
	if err := eng.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	mv, err := eng.Go(ctx)
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if mv.IsPass() {
		t.Fatal("initial position should return a non-pass move")
	}
	if err := eng.PushMove(ctx, mv); err != nil {
		t.Fatalf("PushMove: %v", err)
	}
	if eng.ResetCalls != 1 || eng.GoCalls != 1 || eng.PushCalls != 1 {
		t.Fatalf("unexpected call counts: reset=%d go=%d push=%d", eng.ResetCalls, eng.GoCalls, eng.PushCalls)
	}
	if phaseOf(eng.Board) != 1 {
		t.Fatalf("phase = %d, want 1", phaseOf(eng.Board))
	}
}

func TestMockScriptedMoves(t *testing.T) {
	ctx := context.Background()
	eng := New()
	if err := eng.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	eng.ScriptedMoves = []board.Move{board.Move(mustSquare(t, "f5"))}
	mv, err := eng.Go(ctx)
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if mv.String() != "f5" {
		t.Fatalf("scripted move = %s, want f5", mv)
	}
}

func phaseOf(b board.Board) int {
	black, white, _ := b.DiscCounts()
	return black + white - 4
}

func mustSquare(t *testing.T, s string) board.Square {
	t.Helper()
	sq, err := board.ParseSquare(s)
	if err != nil {
		t.Fatal(err)
	}
	return sq
}
