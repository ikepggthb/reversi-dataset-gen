// Package engine wraps an external Othello engine subprocess (Edax / Egaroucid)
// and exposes a small Engine interface to the rest of the app.
package engine

import (
	"context"
	"fmt"

	"github.com/ikepggthb/reversi-dataset-gen/internal/board"
	"github.com/ikepggthb/reversi-dataset-gen/internal/config"
)

// Engine は外部エンジンの 1 サブプロセスを抽象化する。
//
// 並行性: メソッドは同一インスタンスに対して並行呼び出ししない前提
// (1 worker = 1 engine = 直列呼び出し)。
//
// ctx: worker から渡される master cancellation。
// engine 側でも spec の Timeout で per-call timeout を上乗せする。
type Engine interface {
	// Reset は新しい初期局面でゲームを開始する。
	Reset(ctx context.Context) error
	// PushMove は手 (どちらの手番でも) をエンジンの内部盤に反映する。
	PushMove(ctx context.Context, m board.Move) error
	// Go は現在の局面でエンジンに探索させ、最善手を返す。
	Go(ctx context.Context) (board.Move, error)
	// Restart はサブプロセスを kill して新規起動する。
	Restart(ctx context.Context) error
	// Close はサブプロセスを終了する。
	Close() error
}

// New は config からエンジンを起動して返す。起動 + handshake が完了した状態で返る。
func New(ctx context.Context, cfg config.Engine) (Engine, error) {
	if cfg.Kind == config.EngineEgaroucid {
		return newEgaroucid(ctx, cfg)
	}
	spec, err := buildSpec(cfg)
	if err != nil {
		return nil, err
	}
	return startNBoard(ctx, spec)
}

func buildSpec(cfg config.Engine) (launchSpec, error) {
	switch cfg.Kind {
	case config.EngineEdax:
		return newEdaxSpec(cfg)
	default:
		return launchSpec{}, fmt.Errorf("engine: unknown kind %q", cfg.Kind)
	}
}
