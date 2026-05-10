package engine

import (
	"errors"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ikepggthb/reversi-dataset-gen/internal/config"
)

// newEdaxSpec は Edax 用の launchSpec を返す。
//
// Edax は -nboard モードで起動し、ハンドシェイクは:
//
//	send: nboard 1
//	send: ping 1
//	wait for "pong 1"
//
// 探索結果は "=== F5" のような行で来て、"status Edax is waiting" で完了する。
func newEdaxSpec(cfg config.Engine) (launchSpec, error) {
	if cfg.Binary == "" {
		return launchSpec{}, errors.New("edax: binary is required")
	}
	evalFile, _ := cfg.Options["eval_file"].(string)
	if evalFile == "" {
		// デフォルト: <bin のディレクトリ>/data/eval.dat
		evalFile = filepath.Join(filepath.Dir(cfg.Binary), "data", "eval.dat")
	}

	threads := cfg.Threads
	if threads <= 0 {
		threads = 1
	}

	args := []string{
		cfg.Binary,
		"-nboard",
		"-q",
		"-book-usage", "off",
		"-ponder", "off",
		"-eval-file", evalFile,
		"-n", strconv.Itoa(threads),
		"-l", strconv.Itoa(cfg.Level),
	}

	startupTimeout := cfg.Timeout
	if startupTimeout < 5*time.Second {
		startupTimeout = 5 * time.Second
	}

	return launchSpec{
		Name:           "edax",
		Args:           args,
		Cwd:            filepath.Dir(cfg.Binary),
		StartupSends:   []string{"nboard 1", "ping 1"},
		StartupAck:     "pong 1",
		GoCmd:          "go",
		MovePrefix:     "===",
		DoneAck:        "status Edax is waiting",
		QuitCmd:        "quit",
		StartupTimeout: startupTimeout,
		OpTimeout:      cfg.Timeout,
	}, nil
}

// Edax 用 [engine.options] で受け付けるキー:
//
//	eval_file: 評価関数ファイル (省略時: <binary dir>/data/eval.dat)
