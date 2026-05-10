package dataset

import (
	"context"
	"errors"
	"fmt"
	"math/rand"

	"github.com/ikepggthb/reversi-dataset-gen/internal/board"
	"github.com/ikepggthb/reversi-dataset-gen/internal/engine"
)

type result struct {
	b       board.Board
	hash    string
	value   int16
	ok      bool
	err     error
	metrics metrics
}

func sendResult[T any](ctx context.Context, out chan<- T, v T) bool {
	select {
	case out <- v:
		return true
	case <-ctx.Done():
		return false
	}
}

func reachPhase(ctx context.Context, cfg Config, target int, rng *rand.Rand, eng engine.Engine) (board.Board, metrics, error) {
	b := board.New()
	var met metrics
	if eng != nil {
		if err := eng.Reset(ctx); err != nil {
			met.errors++
			return b, met, err
		}
	}
	for phaseOf(b) < target {
		if err := ctx.Err(); err != nil {
			return b, met, err
		}
		status, legal := b.Status()
		switch status {
		case board.StatusGameOver:
			return b, met, errors.New("game over before target phase")
		case board.StatusPass:
			if err := b.Apply(board.PassMove); err != nil {
				return b, met, err
			}
			if eng != nil {
				// Pass is deterministic from board state; any engine desync is surfaced by the next engine operation.
				_ = eng.PushMove(ctx, board.PassMove)
			}
			met.playoutPass++
			continue
		}
		var mv board.Move
		useAI := eng != nil && rng.Float64() < cfg.Playout.AIProbability
		if useAI {
			got, err := eng.Go(ctx)
			if err != nil {
				met.timeouts++
				return b, met, err
			}
			mv = got
			met.playoutEngine++
		} else {
			mv = board.Move(legal[rng.Intn(len(legal))])
			met.playoutRandom++
		}
		if err := b.Apply(mv); err != nil {
			met.errors++
			return b, met, err
		}
		if eng != nil {
			if err := eng.PushMove(ctx, mv); err != nil {
				met.errors++
				return b, met, err
			}
		}
	}
	return b, met, nil
}

func finishGame(ctx context.Context, eng engine.Engine, moves string, stm board.Player, retries int) (int16, metrics, error) {
	var last error
	var total metrics
	for attempt := 0; attempt <= retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return 0, total, err
		}
		met := metrics{selfGames: 1}
		if attempt > 0 {
			if err := eng.Restart(ctx); err != nil {
				met.errors++
				addMetrics(&total, met)
				return 0, total, err
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
		for {
			if err := ctx.Err(); err != nil {
				addMetrics(&total, met)
				return 0, total, err
			}
			status, _ := b.Status()
			switch status {
			case board.StatusGameOver:
				black, white, _ := b.DiscCounts()
				margin := black - white
				if stm == board.White {
					margin = -margin
				}
				addMetrics(&total, met)
				return int16(margin), total, nil
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
		}
		addMetrics(&total, met)
	}
	total.failed = 1
	return 0, total, last
}

func replayToEngine(ctx context.Context, eng engine.Engine, b *board.Board, moves string) error {
	if len(moves)%2 != 0 {
		return fmt.Errorf("moves length must be even")
	}
	for i := 0; i < len(moves); i += 2 {
		for {
			status, _ := b.Status()
			if status != board.StatusPass {
				break
			}
			if err := b.Apply(board.PassMove); err != nil {
				return err
			}
			if err := eng.PushMove(ctx, board.PassMove); err != nil {
				return err
			}
		}
		mv, err := board.ParseMove(moves[i : i+2])
		if err != nil {
			return err
		}
		if err := b.Apply(mv); err != nil {
			return err
		}
		if err := eng.PushMove(ctx, mv); err != nil {
			return err
		}
	}
	return nil
}
