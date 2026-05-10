// Command rdg generates Othello phase datasets.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ikepggthb/reversi-dataset-gen/internal/config"
	"github.com/ikepggthb/reversi-dataset-gen/internal/dataset"
	"github.com/ikepggthb/reversi-dataset-gen/internal/identity"
	"github.com/ikepggthb/reversi-dataset-gen/internal/runui"
	"github.com/ikepggthb/reversi-dataset-gen/internal/setup"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("rdg", flag.ExitOnError)
	configPath := fs.String("config", "configs/config.toml", "dataset generation config")
	appConfig := fs.String("app-config", "configs/app.toml", "application engine registry config")
	resume := fs.Bool("resume", false, "resume compatible incomplete phase outputs")
	force := fs.Bool("force", false, "delete existing phase outputs before generating")
	repair := fs.Bool("repair", false, "with --resume, rebuild phase outputs that are missing metadata.json")
	dryRun := fs.Bool("dry-run", false, "estimate work without generating datasets")
	yes := fs.Bool("yes", false, "answer yes to engine setup prompts")
	noProgress := fs.Bool("no-progress", false, "print line logs instead of the interactive dashboard")
	jsonProgress := fs.Bool("json-progress", false, "emit progress as JSONL")
	logFile := fs.String("log-file", "", "override app.toml [log].file and write plain progress logs to this file")
	noColor := fs.Bool("no-color", false, "disable ANSI color")
	noEmoji := fs.Bool("no-emoji", false, "disable emoji markers")
	progressInterval := fs.Duration("progress-interval", 1*time.Second, "progress dashboard refresh interval")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: rdg [--config configs/config.toml] [--app-config configs/app.toml] [--resume|--force] [--dry-run]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}
	if *resume && *force {
		fmt.Fprintln(os.Stderr, "rdg: --resume and --force cannot be used together")
		return 2
	}
	if *repair && !*resume {
		fmt.Fprintln(os.Stderr, "rdg: --repair requires --resume")
		return 2
	}
	if *noProgress && *jsonProgress {
		fmt.Fprintln(os.Stderr, "rdg: --no-progress and --json-progress cannot be used together")
		return 2
	}
	cfg, err := dataset.LoadConfig(*configPath, *appConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	forceExitOnSecondSignal(ctx, stop)
	if !*dryRun {
		if err := prepareEngines(ctx, cfg.EngineConfig, setup.Options{AssumeYes: *yes}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := identity.Populate(ctx, cfg.EngineConfig); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	var progressCh chan<- runui.Event
	var done <-chan struct{}
	if *jsonProgress {
		progressCh, done = startJSONProgress(ctx)
	} else if *noProgress {
		progressCh, done = startLineProgress(ctx, *progressInterval)
	} else {
		progressCh, done = startUI(ctx, true, *progressInterval)
	}
	effectiveLogFile := cfg.EngineConfig.LogFile
	logMaxBytes := cfg.EngineConfig.LogMaxBytes
	logMaxFiles := cfg.EngineConfig.LogMaxFiles
	if *logFile != "" {
		effectiveLogFile = *logFile
	}
	if effectiveLogFile != "" {
		var err error
		progressCh, done, err = addFileLogSink(ctx, progressCh, done, effectiveLogFile, logMaxBytes, logMaxFiles, *progressInterval)
		if err != nil {
			closeUI(progressCh, done)
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	if err := dataset.Run(ctx, cfg, dataset.Options{
		Force: *force, Resume: *resume, Repair: *repair, DryRun: *dryRun,
		Progress: progressCh, ProgressJSON: *jsonProgress, NoProgress: *noProgress,
		NoEmoji: *noEmoji, NoColor: *noColor || os.Getenv("NO_COLOR") != "",
	}); err != nil {
		sendFatalProgress(progressCh, err, *noEmoji, *noColor || os.Getenv("NO_COLOR") != "")
		closeUI(progressCh, done)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	closeUI(progressCh, done)
	return 0
}

func startLineProgress(ctx context.Context, interval time.Duration) (chan<- runui.Event, <-chan struct{}) {
	ch := make(chan runui.Event, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runui.LineLoop(ctx, ch, interval, os.Stderr)
	}()
	return ch, done
}

func prepareEngines(ctx context.Context, cfg *config.Config, opts setup.Options) error {
	if cfg.Setup == "off" {
		return nil
	}
	names := cfg.UsedEngineNames()
	ranKind := map[config.EngineKind]bool{}
	for _, name := range names {
		eng := cfg.Engines[name]
		switch cfg.Setup {
		case "always":
			if ranKind[eng.Kind] {
				continue
			}
			if err := setup.Run(ctx, eng.Kind, opts); err != nil {
				return err
			}
			ranKind[eng.Kind] = true
		default:
			if err := setup.Ensure(ctx, eng, opts); err != nil {
				return err
			}
		}
	}
	return nil
}

func startUI(ctx context.Context, enabled bool, interval time.Duration) (chan<- runui.Event, <-chan struct{}) {
	if !enabled {
		return nil, nil
	}
	ch := make(chan runui.Event, 32)
	done := make(chan struct{})
	state := runui.NewState()
	go func() {
		defer close(done)
		runui.Loop(ctx, state, ch, interval, os.Stderr)
	}()
	return ch, done
}

func closeUI(ch chan<- runui.Event, done <-chan struct{}) {
	if ch != nil {
		close(ch)
	}
	if done != nil {
		<-done
	}
}

func sendFatalProgress(ch chan<- runui.Event, err error, noEmoji, noColor bool) {
	if ch == nil || err == nil {
		return
	}
	title := "❌ rdg fatal"
	if noEmoji {
		title = "rdg fatal"
	}
	ev := runui.Event{Stage: "fatal", Title: title, Detail: err.Error(), NoEmoji: noEmoji, NoColor: noColor}
	select {
	case ch <- ev:
	case <-time.After(500 * time.Millisecond):
	}
}

func forceExitOnSecondSignal(ctx context.Context, stop context.CancelFunc) {
	go func() {
		<-ctx.Done()
		stop()
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(sig)
		<-sig
		os.Exit(130)
	}()
}

func addFileLogSink(ctx context.Context, primary chan<- runui.Event, primaryDone <-chan struct{}, path string, maxBytes int64, maxFiles int, interval time.Duration) (chan<- runui.Event, <-chan struct{}, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, fmt.Errorf("create log dir: %w", err)
		}
	}
	if err := rotateLogIfNeeded(path, maxBytes, maxFiles); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}
	logCh, logDone := startFileProgressLog(ctx, f, interval)
	in := make(chan runui.Event, 32)
	primaryFeed := make(chan runui.Event, 32)
	logFeed := make(chan runui.Event, 32)
	done := make(chan struct{})
	sinkDone := make(chan struct{}, 2)
	sinkCount := 1
	if primary != nil {
		sinkCount++
		go func() {
			defer func() { sinkDone <- struct{}{} }()
			for ev := range primaryFeed {
				select {
				case primary <- ev:
				case <-ctx.Done():
					close(primary)
					if primaryDone != nil {
						<-primaryDone
					}
					return
				}
			}
			close(primary)
			if primaryDone != nil {
				<-primaryDone
			}
		}()
	}
	go func() {
		defer func() { sinkDone <- struct{}{} }()
		for ev := range logFeed {
			select {
			case logCh <- ev:
			case <-ctx.Done():
				close(logCh)
				<-logDone
				_ = f.Close()
				return
			}
		}
		close(logCh)
		<-logDone
		_ = f.Close()
	}()
	go func() {
		defer close(done)
		dropped := 0
		cleanup := func() {
			if primary != nil {
				close(primaryFeed)
			}
			close(logFeed)
			for i := 0; i < sinkCount; i++ {
				<-sinkDone
			}
			if dropped > 0 {
				fmt.Fprintf(os.Stderr, "log sink dropped %d events\n", dropped)
			}
		}
		for {
			select {
			case ev, ok := <-in:
				if !ok {
					cleanup()
					return
				}
				if primary != nil {
					select {
					case primaryFeed <- ev:
					default:
						dropped++
					}
				}
				select {
				case logFeed <- ev:
				default:
					dropped++
				}
			case <-ctx.Done():
				cleanup()
				return
			}
		}
	}()
	return in, done, nil
}

func rotateLogIfNeeded(path string, maxBytes int64, maxFiles int) error {
	if maxBytes <= 0 || maxFiles <= 0 {
		return nil
	}
	st, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat log file: %w", err)
	}
	if st.Size() < maxBytes {
		return nil
	}
	oldest := fmt.Sprintf("%s.%d", path, maxFiles)
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove old log: %w", err)
	}
	for i := maxFiles - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", path, i)
		dst := fmt.Sprintf("%s.%d", path, i+1)
		if err := os.Rename(src, dst); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rotate log %s to %s: %w", src, dst, err)
		}
	}
	if err := os.Rename(path, path+".1"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("rotate log %s to %s: %w", path, path+".1", err)
	}
	return nil
}

func startFileProgressLog(ctx context.Context, f *os.File, interval time.Duration) (chan<- runui.Event, <-chan struct{}) {
	ch := make(chan runui.Event, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runui.LineLoop(ctx, ch, interval, f)
	}()
	return ch, done
}

func startJSONProgress(ctx context.Context) (chan<- runui.Event, <-chan struct{}) {
	ch := make(chan runui.Event, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		enc := json.NewEncoder(os.Stderr)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				_ = enc.Encode(ev)
			}
		}
	}()
	return ch, done
}
