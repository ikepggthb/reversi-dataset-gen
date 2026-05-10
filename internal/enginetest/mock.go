// Package enginetest provides test doubles for engine.Engine.
package enginetest

import (
	"context"
	"errors"
	"fmt"

	"github.com/ikepggthb/reversi-dataset-gen/internal/board"
	"github.com/ikepggthb/reversi-dataset-gen/internal/engine"
)

var _ engine.Engine = (*Mock)(nil)

// Mock is an in-memory engine.Engine implementation for tests.
//
// Go returns ScriptedMoves in order when provided. Otherwise it calls GoFunc,
// or falls back to the first legal move in the local board position.
type Mock struct {
	Board board.Board

	ScriptedMoves []board.Move
	GoFunc        func(board.Board) (board.Move, error)
	GoWithContext func(context.Context, board.Board) (board.Move, error)

	ResetErr   error
	PushErr    error
	GoErr      error
	RestartErr error
	CloseErr   error

	ResetCalls   int
	PushCalls    int
	GoCalls      int
	RestartCalls int
	CloseCalls   int
}

// New returns a Mock reset to the initial board.
func New() *Mock {
	m := &Mock{}
	m.Board = board.New()
	return m
}

func (m *Mock) Reset(ctx context.Context) error {
	m.ResetCalls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.ResetErr != nil {
		return m.ResetErr
	}
	m.Board = board.New()
	return nil
}

func (m *Mock) PushMove(ctx context.Context, mv board.Move) error {
	m.PushCalls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.PushErr != nil {
		return m.PushErr
	}
	if err := m.Board.Apply(mv); err != nil {
		return fmt.Errorf("mock engine push %s: %w", mv, err)
	}
	return nil
}

func (m *Mock) Go(ctx context.Context) (board.Move, error) {
	m.GoCalls++
	if err := ctx.Err(); err != nil {
		return board.PassMove, err
	}
	if m.GoErr != nil {
		return board.PassMove, m.GoErr
	}
	if len(m.ScriptedMoves) > 0 {
		mv := m.ScriptedMoves[0]
		m.ScriptedMoves = m.ScriptedMoves[1:]
		return mv, nil
	}
	if m.GoWithContext != nil {
		return m.GoWithContext(ctx, m.Board)
	}
	if m.GoFunc != nil {
		return m.GoFunc(m.Board)
	}
	status, legal := m.Board.Status()
	switch status {
	case board.StatusPlay:
		return board.Move(legal[0]), nil
	case board.StatusPass:
		return board.PassMove, nil
	case board.StatusGameOver:
		return board.PassMove, errors.New("mock engine go called after game over")
	default:
		return board.PassMove, errors.New("mock engine unknown board status")
	}
}

func (m *Mock) Restart(ctx context.Context) error {
	m.RestartCalls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.RestartErr != nil {
		return m.RestartErr
	}
	return m.Reset(ctx)
}

func (m *Mock) Close() error {
	m.CloseCalls++
	return m.CloseErr
}
