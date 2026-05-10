// Package setup runs repository setup targets needed before generation.
package setup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/ikepggthb/reversi-dataset-gen/internal/config"
	"github.com/ikepggthb/reversi-dataset-gen/internal/identity"
)

// Options controls setup command execution.
type Options struct {
	Stdout    io.Writer
	Stderr    io.Writer
	Stdin     io.Reader
	AssumeYes bool
}

// Ensure runs setup only when required engine files are missing or unusable.
func Ensure(ctx context.Context, eng config.Engine, opts Options) error {
	missing, err := missingRequirements(eng)
	if err != nil {
		return err
	}
	if len(missing) == 0 {
		return nil
	}
	errOut := opts.Stderr
	if errOut == nil {
		errOut = os.Stderr
	}
	fmt.Fprintf(errOut, "engine setup required for %s: %v\n", eng.Kind, missing)
	if !opts.AssumeYes {
		ok, err := confirm(opts, fmt.Sprintf("Run make setup-%s now?", eng.Kind))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("auto setup declined; missing requirements: %v", missing)
		}
	}
	return Run(ctx, eng.Kind, opts)
}

// Run executes the Makefile setup target for the configured engine kind.
func Run(ctx context.Context, kind config.EngineKind, opts Options) error {
	target, err := targetForKind(kind)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "make", target)
	cmd.Stdout = opts.Stdout
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = opts.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("auto setup %s: %w", target, err)
	}
	return nil
}

func confirm(opts Options, prompt string) (bool, error) {
	in := opts.Stdin
	if in == nil {
		in = os.Stdin
	}
	out := opts.Stderr
	if out == nil {
		out = os.Stderr
	}
	fmt.Fprintf(out, "%s [y/N]: ", prompt)
	sc := bufio.NewScanner(in)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return false, err
		}
		return false, nil
	}
	answer := strings.ToLower(strings.TrimSpace(sc.Text()))
	return answer == "y" || answer == "yes", nil
}

func targetForKind(kind config.EngineKind) (string, error) {
	switch kind {
	case config.EngineEdax:
		return "setup-edax", nil
	case config.EngineEgaroucid:
		return "setup-egaroucid", nil
	default:
		return "", fmt.Errorf("auto setup: unsupported engine kind %q", kind)
	}
}

type requirement struct {
	path       string
	executable bool
	label      string
}

func requirements(eng config.Engine) []requirement {
	reqs := []requirement{{path: eng.Binary, executable: true, label: "engine binary"}}
	if eval := identity.EvalFilePath(eng); eval != "" {
		reqs = append(reqs, requirement{path: eval, label: "eval file"})
	}
	return reqs
}

func missingRequirements(eng config.Engine) ([]string, error) {
	var missing []string
	for _, req := range requirements(eng) {
		if req.path == "" {
			missing = append(missing, req.label+" path is empty")
			continue
		}
		st, err := os.Stat(req.path)
		if err != nil {
			if os.IsNotExist(err) {
				missing = append(missing, req.label+" missing: "+req.path)
				continue
			}
			return nil, fmt.Errorf("auto setup: stat %s: %w", req.path, err)
		}
		if st.IsDir() {
			missing = append(missing, req.label+" is a directory: "+req.path)
			continue
		}
		if req.executable && st.Mode()&0o111 == 0 {
			missing = append(missing, req.label+" is not executable: "+req.path)
		}
	}
	return missing, nil
}
