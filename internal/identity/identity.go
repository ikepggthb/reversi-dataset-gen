// Package identity computes reproducibility metadata for a generation run.
package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ikepggthb/reversi-dataset-gen/internal/config"
)

// Populate fills cfg.RunIdentity from the configured engine files and git state.
func Populate(ctx context.Context, cfg *config.Config) error {
	generatorCommit, generatorDirty, generatorDirtyKnown := Generator(ctx)

	cfg.RunIdentity = config.RunIdentity{
		Engines:             map[string]config.EngineIdentity{},
		GeneratorCommit:     generatorCommit,
		GeneratorDirty:      generatorDirty,
		GeneratorDirtyKnown: generatorDirtyKnown,
	}
	for _, name := range cfg.UsedEngineNames() {
		eng := cfg.Engines[name]
		ident, err := engineIdentity(ctx, eng)
		if err != nil {
			return fmt.Errorf("identity: engine %s: %w", name, err)
		}
		cfg.RunIdentity.Engines[name] = ident
		if cfg.RunIdentity.EngineBinarySHA256 == "" {
			cfg.RunIdentity.EngineRef = ident.EngineRef
			cfg.RunIdentity.EngineBinarySHA256 = ident.EngineBinarySHA256
			cfg.RunIdentity.EvalFileSHA256 = ident.EvalFileSHA256
		}
	}
	return nil
}

// Generator returns git identity metadata for this generator checkout.
func Generator(ctx context.Context) (commit string, dirty bool, dirtyKnown bool) {
	generatorCommit, _ := gitOutput(ctx, ".", "rev-parse", "HEAD")
	if status, err := gitOutput(ctx, ".", "status", "--short", "--", "."); err == nil {
		dirtyKnown = true
		dirty = strings.TrimSpace(status) != ""
	}
	return strings.TrimSpace(generatorCommit), dirty, dirtyKnown
}

func engineIdentity(ctx context.Context, eng config.Engine) (config.EngineIdentity, error) {
	engineSHA, err := FileSHA256(eng.Binary)
	if err != nil {
		return config.EngineIdentity{}, fmt.Errorf("binary sha256: %w", err)
	}
	evalPath := EvalFilePath(eng)
	evalSHA := ""
	if evalPath != "" {
		evalSHA, err = FileSHA256(evalPath)
		if err != nil {
			return config.EngineIdentity{}, fmt.Errorf("eval file sha256: %w", err)
		}
	}
	engineRef, _ := gitOutput(ctx, filepath.Dir(eng.Binary), "rev-parse", "HEAD")
	return config.EngineIdentity{
		EngineRef:          strings.TrimSpace(engineRef),
		EngineBinarySHA256: engineSHA,
		EvalFileSHA256:     evalSHA,
	}, nil
}

// EvalFilePath returns the eval file path used by the configured engine, if any.
func EvalFilePath(eng config.Engine) string {
	if s, _ := eng.Options["eval_file"].(string); s != "" {
		return s
	}
	switch eng.Kind {
	case config.EngineEdax:
		return filepath.Join(filepath.Dir(eng.Binary), "data", "eval.dat")
	case config.EngineEgaroucid:
		return filepath.Join(filepath.Dir(eng.Binary), "resources", "eval.egev2")
	default:
		return ""
	}
}

// FileSHA256 returns the hex SHA-256 digest of a file.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}
