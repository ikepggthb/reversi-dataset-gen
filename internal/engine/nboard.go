package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ikepggthb/reversi-dataset-gen/internal/board"
)

// launchSpec はエンジン起動コマンドと NBoard プロトコル差分。
type launchSpec struct {
	Args []string // argv[0] が実行ファイル
	Cwd  string

	// 起動 handshake: 順に StartupSends を送って StartupAck (完全一致) を待つ
	StartupSends []string
	StartupAck   string

	// プロトコル
	GoCmd      string // 探索開始 (例: "go")
	MovePrefix string // 探索結果行のプレフィックス (例: "===")
	DoneAck    string // 探索終了を示す line (完全一致)
	QuitCmd    string // 正常終了

	// タイムアウト
	StartupTimeout time.Duration
	OpTimeout      time.Duration // per-call (Reset/PushMove/Go) のタイムアウト

	// 表示用 (エラーメッセージで使う)
	Name string
}

// nboardEngine は NBoard プロトコルでサブプロセスを制御する。
type nboardEngine struct {
	spec launchSpec

	// 以下のフィールドは spawn のたびに作り直す。
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	pipeReader *io.PipeReader
	pipeWriter *io.PipeWriter
	lines      chan string   // stdout/stderr マージ済みの行
	done       chan struct{} // cmd.Wait が返ったら close される
	waitErr    error         // cmd.Wait の戻り値
	readErr    error         // Scanner の戻り値

	stateMu    sync.Mutex
	generation int
	closeOnce  sync.Once
}

// startNBoard はサブプロセスを起動 + handshake して engine を返す。
func startNBoard(ctx context.Context, spec launchSpec) (*nboardEngine, error) {
	e := &nboardEngine{spec: spec}
	if err := e.spawn(ctx); err != nil {
		return nil, err
	}
	return e, nil
}

// spawn はサブプロセスを起動し、stdout/stderr の reader goroutine を立ち上げ、handshake する。
// 失敗した場合は subprocess を後始末してから error を返す。
func (e *nboardEngine) spawn(ctx context.Context) error {
	if len(e.spec.Args) == 0 {
		return errors.New("engine: empty args")
	}

	cmd := exec.Command(e.spec.Args[0], e.spec.Args[1:]...)
	cmd.Dir = e.spec.Cwd

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("engine %s: stdin pipe: %w", e.spec.Name, err)
	}

	// stdout / stderr を 1 本のパイプにマージする。
	// (Edax はエラーを stderr に出すことがあるため、両方を line stream として扱う)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = pw.Close()
		_ = pr.Close()
		return fmt.Errorf("engine %s: start: %w", e.spec.Name, err)
	}

	e.cmd = cmd
	e.stdin = stdin
	e.pipeReader = pr
	e.pipeWriter = pw
	e.lines = make(chan string, 256)
	e.done = make(chan struct{})
	linesCh := e.lines
	doneCh := e.done
	pwLocal := pw
	prLocal := pr
	e.stateMu.Lock()
	e.generation++
	gen := e.generation
	e.waitErr = nil
	e.readErr = nil
	e.stateMu.Unlock()

	// Wait → pipe close goroutine
	// cmd.Wait はプロセス終了まで blocking。終了後にパイプの writer 側を閉じる。
	// それにより reader goroutine の bufio.Scanner が EOF を検出して終了する。
	go func() {
		err := cmd.Wait()
		e.stateMu.Lock()
		if e.generation == gen {
			e.waitErr = err
		}
		e.stateMu.Unlock()
		_ = pwLocal.Close()
		close(doneCh)
	}()

	// Reader goroutine: bufio.Scanner で行ごとに lines に流す。
	go func() {
		defer close(linesCh)
		scanner := bufio.NewScanner(prLocal)
		// Edax の出力に長い行が来る可能性があるため buffer を拡張
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for scanner.Scan() {
			linesCh <- scanner.Text()
		}
		e.stateMu.Lock()
		if e.generation == gen {
			e.readErr = scanner.Err()
		}
		e.stateMu.Unlock()
	}()

	// handshake
	startupCtx := ctx
	if e.spec.StartupTimeout > 0 {
		var cancel context.CancelFunc
		startupCtx, cancel = context.WithTimeout(ctx, e.spec.StartupTimeout)
		defer cancel()
	}
	for _, line := range e.spec.StartupSends {
		if err := e.sendLine(line); err != nil {
			e.shutdown()
			return fmt.Errorf("engine %s: handshake send %q: %w", e.spec.Name, line, err)
		}
	}
	if err := e.waitForExact(startupCtx, e.spec.StartupAck); err != nil {
		e.shutdown()
		return fmt.Errorf("engine %s: handshake: %w", e.spec.Name, err)
	}
	return nil
}

// sendLine は engine に 1 行送信する (改行は自動付与)。
func (e *nboardEngine) sendLine(line string) error {
	if e.stdin == nil {
		return errors.New("engine: stdin is closed")
	}
	if _, err := io.WriteString(e.stdin, line+"\n"); err != nil {
		return err
	}
	return nil
}

// readLine は ctx の範囲内で次の line を返す。
// ctx 失効時はサブプロセスを kill して ctx.Err() を返す。
func (e *nboardEngine) readLine(ctx context.Context) (string, error) {
	select {
	case line, ok := <-e.lines:
		if !ok {
			return "", e.pipeClosedError()
		}
		return line, nil
	case <-ctx.Done():
		// engine を解放する。次回呼び出しは Restart が必要になる。
		e.shutdown()
		return "", ctx.Err()
	}
}

// waitForExact は line が exact 一致するまで読み飛ばす。
func (e *nboardEngine) waitForExact(ctx context.Context, want string) error {
	for {
		line, err := e.readLine(ctx)
		if err != nil {
			return err
		}
		if strings.TrimSpace(line) == want {
			return nil
		}
	}
}

func (e *nboardEngine) pipeClosedError() error {
	e.stateMu.Lock()
	readErr := e.readErr
	waitErr := e.waitErr
	e.stateMu.Unlock()
	// cmd.Wait が早期に返っている場合はその情報を返す。
	select {
	case <-e.done:
		if readErr != nil {
			return fmt.Errorf("engine %s output scanner: %w", e.spec.Name, readErr)
		}
		if waitErr != nil {
			return fmt.Errorf("engine %s pipe closed: %w", e.spec.Name, waitErr)
		}
		return fmt.Errorf("engine %s exited", e.spec.Name)
	default:
		if readErr != nil {
			return fmt.Errorf("engine %s output scanner: %w", e.spec.Name, readErr)
		}
		return fmt.Errorf("engine %s pipe closed unexpectedly", e.spec.Name)
	}
}

// withOpTimeout は ctx に spec.OpTimeout を上乗せした context を返す。
func (e *nboardEngine) withOpTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if e.spec.OpTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, e.spec.OpTimeout)
}

// ---------- Engine 実装 ----------

// Reset は NBoard の game command を送信するだけで応答を待たない。
// NBoard 側は reset ack を返さないため、送信失敗だけを同期的に返す。
func (e *nboardEngine) Reset(ctx context.Context) error {
	ctx, cancel := e.withOpTimeout(ctx)
	defer cancel()
	if err := e.sendLine("game (;GM[othello];)"); err != nil {
		return fmt.Errorf("engine %s: reset send: %w", e.spec.Name, err)
	}
	_ = ctx // Reset は応答を待たない (NBoard 仕様)。ctx は未使用だが署名統一のため受ける。
	return nil
}

func (e *nboardEngine) PushMove(ctx context.Context, m board.Move) error {
	_ = ctx
	if err := e.sendLine("move " + m.String()); err != nil {
		return fmt.Errorf("engine %s: push move: %w", e.spec.Name, err)
	}
	return nil
}

func (e *nboardEngine) Go(ctx context.Context) (board.Move, error) {
	ctx, cancel := e.withOpTimeout(ctx)
	defer cancel()

	if err := e.sendLine(e.spec.GoCmd); err != nil {
		return 0, fmt.Errorf("engine %s: go send: %w", e.spec.Name, err)
	}

	var picked *board.Move
	for {
		line, err := e.readLine(ctx)
		if err != nil {
			return 0, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Error:") || strings.HasPrefix(line, "ERROR:") {
			return 0, fmt.Errorf("engine %s error: %s", e.spec.Name, line)
		}
		if strings.HasPrefix(line, e.spec.MovePrefix) {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				return 0, fmt.Errorf("engine %s: malformed move line %q", e.spec.Name, line)
			}
			m, perr := board.ParseMove(strings.ToLower(parts[1]))
			if perr != nil {
				return 0, fmt.Errorf("engine %s: parse move %q: %w", e.spec.Name, parts[1], perr)
			}
			picked = &m
			continue
		}
		if line == e.spec.DoneAck && picked != nil {
			return *picked, nil
		}
	}
}

func (e *nboardEngine) Restart(ctx context.Context) error {
	e.shutdown()
	return e.spawn(ctx)
}

func (e *nboardEngine) Close() error {
	var err error
	e.closeOnce.Do(func() {
		// Graceful: quit を送って stdin を閉じ、終了を一定時間待つ
		if e.stdin != nil {
			_, _ = io.WriteString(e.stdin, e.spec.QuitCmd+"\n")
			_ = e.stdin.Close()
		}
		select {
		case <-e.done:
		case <-time.After(2 * time.Second):
			err = e.killAndReap()
		}
	})
	return err
}

// shutdown はサブプロセスを強制終了して回収する。複数回呼んでも安全。
func (e *nboardEngine) shutdown() {
	if e.cmd == nil || e.cmd.Process == nil {
		return
	}
	_ = e.killAndReap()
}

func (e *nboardEngine) killAndReap() error {
	if e.cmd == nil || e.cmd.Process == nil {
		return nil
	}
	if e.stdin != nil {
		_ = e.stdin.Close()
	}
	_ = e.cmd.Process.Kill()
	if e.pipeWriter != nil {
		_ = e.pipeWriter.Close()
	}
	select {
	case <-e.done:
	case <-time.After(2 * time.Second):
		return fmt.Errorf("engine %s: failed to reap subprocess", e.spec.Name)
	}
	return nil
}
