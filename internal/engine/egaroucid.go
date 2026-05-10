package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikepggthb/reversi-dataset-gen/internal/board"
	"github.com/ikepggthb/reversi-dataset-gen/internal/config"
)

// egaroucidEngine controls Egaroucid for Console through GTP mode.
//
// Go uses reg_genmove so Egaroucid reports a best move without changing its
// internal board. The worker then calls PushMove for every played move, keeping
// the same Engine contract as Edax.
type egaroucidEngine struct {
	cfg    config.Engine
	args   []string
	cwd    string
	player board.Player
	nextID int

	cmd        *exec.Cmd
	stdin      io.WriteCloser
	pipeWriter *io.PipeWriter
	lines      chan string
	done       chan struct{}
	waitErr    error
	readErr    error

	stateMu    sync.Mutex
	generation int
	closeOnce  sync.Once
}

func newEgaroucid(ctx context.Context, cfg config.Engine) (Engine, error) {
	if cfg.Binary == "" {
		return nil, errors.New("egaroucid: binary is required")
	}
	threads := cfg.Threads
	if threads <= 0 {
		threads = 1
	}
	args := []string{
		cfg.Binary,
		"-gtp",
		"-q",
		"-nobook",
		"-l", strconv.Itoa(cfg.Level),
		"-t", strconv.Itoa(threads),
	}
	if evalFile, _ := cfg.Options["eval_file"].(string); evalFile != "" {
		args = append(args, "-eval", evalFile)
	}
	e := &egaroucidEngine{
		cfg:    cfg,
		args:   args,
		cwd:    filepath.Dir(cfg.Binary),
		player: board.Black,
		nextID: 1,
	}
	if err := e.spawn(ctx); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *egaroucidEngine) spawn(ctx context.Context) error {
	cmd := exec.Command(e.args[0], e.args[1:]...)
	cmd.Dir = e.cwd
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("engine egaroucid: stdin pipe: %w", err)
	}
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = pw.Close()
		_ = pr.Close()
		return fmt.Errorf("engine egaroucid: start: %w", err)
	}

	e.cmd = cmd
	e.stdin = stdin
	e.pipeWriter = pw
	e.lines = make(chan string, 256)
	e.done = make(chan struct{})
	e.player = board.Black
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
	go func() {
		defer close(linesCh)
		scanner := bufio.NewScanner(prLocal)
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

	startupCtx := ctx
	startupTimeout := e.cfg.Timeout
	if startupTimeout < 5*time.Second {
		startupTimeout = 5 * time.Second
	}
	var cancel context.CancelFunc
	startupCtx, cancel = context.WithTimeout(startupCtx, startupTimeout)
	defer cancel()
	if _, err := e.transact(startupCtx, "protocol_version"); err != nil {
		e.shutdown()
		return fmt.Errorf("engine egaroucid: handshake: %w", err)
	}
	return nil
}

func (e *egaroucidEngine) Reset(ctx context.Context) error {
	ctx, cancel := e.withOpTimeout(ctx)
	defer cancel()
	if _, err := e.transact(ctx, "clear_board"); err != nil {
		return fmt.Errorf("engine egaroucid: reset: %w", err)
	}
	e.player = board.Black
	return nil
}

func (e *egaroucidEngine) PushMove(ctx context.Context, m board.Move) error {
	ctx, cancel := e.withOpTimeout(ctx)
	defer cancel()
	move := strings.ToUpper(m.String())
	if m.IsPass() {
		move = "PASS"
	}
	if _, err := e.transact(ctx, "play "+gtpColor(e.player)+" "+move); err != nil {
		return fmt.Errorf("engine egaroucid: push move %s: %w", m, err)
	}
	e.player = e.player.Opponent()
	return nil
}

func (e *egaroucidEngine) Go(ctx context.Context) (board.Move, error) {
	ctx, cancel := e.withOpTimeout(ctx)
	defer cancel()
	res, err := e.transact(ctx, "reg_genmove "+gtpColor(e.player))
	if err != nil {
		return 0, fmt.Errorf("engine egaroucid: go: %w", err)
	}
	if strings.EqualFold(res, "PASS") {
		return board.PassMove, nil
	}
	m, err := board.ParseMove(strings.ToLower(res))
	if err != nil {
		return 0, fmt.Errorf("engine egaroucid: parse move %q: %w", res, err)
	}
	return m, nil
}

func (e *egaroucidEngine) Restart(ctx context.Context) error {
	e.shutdown()
	return e.spawn(ctx)
}

func (e *egaroucidEngine) Close() error {
	var err error
	e.closeOnce.Do(func() {
		if e.stdin != nil {
			_, _ = io.WriteString(e.stdin, "quit\n")
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

func (e *egaroucidEngine) transact(ctx context.Context, command string) (string, error) {
	id := e.nextID
	e.nextID++
	if _, err := io.WriteString(e.stdin, fmt.Sprintf("%d %s\n", id, command)); err != nil {
		return "", err
	}
	wantOK := "=" + strconv.Itoa(id)
	wantErr := "?" + strconv.Itoa(id)
	for {
		line, err := e.readLine(ctx)
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if data, ok := parseGTPResponseLine(line, wantOK); ok {
			return data, nil
		}
		if data, ok := parseGTPResponseLine(line, wantErr); ok {
			return "", fmt.Errorf("gtp error: %s", data)
		}
	}
}

func parseGTPResponseLine(line, prefix string) (string, bool) {
	if line == prefix {
		return "", true
	}
	if strings.HasPrefix(line, prefix+" ") {
		return strings.TrimSpace(strings.TrimPrefix(line, prefix+" ")), true
	}
	return "", false
}

func (e *egaroucidEngine) readLine(ctx context.Context) (string, error) {
	select {
	case line, ok := <-e.lines:
		if !ok {
			return "", e.pipeClosedError()
		}
		return line, nil
	case <-ctx.Done():
		e.shutdown()
		return "", ctx.Err()
	}
}

func (e *egaroucidEngine) withOpTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if e.cfg.Timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, e.cfg.Timeout)
}

func (e *egaroucidEngine) pipeClosedError() error {
	e.stateMu.Lock()
	readErr := e.readErr
	waitErr := e.waitErr
	e.stateMu.Unlock()
	select {
	case <-e.done:
		if readErr != nil {
			return fmt.Errorf("engine egaroucid output scanner: %w", readErr)
		}
		if waitErr != nil {
			return fmt.Errorf("engine egaroucid pipe closed: %w", waitErr)
		}
		return errors.New("engine egaroucid exited")
	default:
		if readErr != nil {
			return fmt.Errorf("engine egaroucid output scanner: %w", readErr)
		}
		return errors.New("engine egaroucid pipe closed unexpectedly")
	}
}

func (e *egaroucidEngine) shutdown() {
	if e.cmd == nil || e.cmd.Process == nil {
		return
	}
	_ = e.killAndReap()
}

func (e *egaroucidEngine) killAndReap() error {
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
		return errors.New("engine egaroucid: failed to reap subprocess")
	}
	return nil
}

func gtpColor(p board.Player) string {
	if p == board.Black {
		return "B"
	}
	return "W"
}
