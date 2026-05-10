// Package config loads application engine registry configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type EngineKind string

const (
	EngineEdax      EngineKind = "edax"
	EngineEgaroucid EngineKind = "egaroucid"
)

type Engine struct {
	Name    string
	Kind    EngineKind
	Binary  string
	Level   int
	Threads int
	Timeout time.Duration
	Options map[string]any
}

type Config struct {
	Setup       string
	LogFile     string
	LogMaxBytes int64
	LogMaxFiles int
	Engines     map[string]Engine
	RunIdentity RunIdentity
}

func (c *Config) UsedEngineNames() []string {
	out := make([]string, 0, len(c.Engines))
	for name := range c.Engines {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

type RunIdentity struct {
	Engines             map[string]EngineIdentity
	GeneratorCommit     string
	GeneratorDirty      bool
	GeneratorDirtyKnown bool

	EngineRef          string
	EngineBinarySHA256 string
	EvalFileSHA256     string
}

type EngineIdentity struct {
	EngineRef          string
	EngineBinarySHA256 string
	EvalFileSHA256     string
}

type rawConfig struct {
	Setup   string               `toml:"setup"`
	Log     rawLog               `toml:"log"`
	Engines map[string]rawEngine `toml:"engines"`
}

type rawLog struct {
	File      string `toml:"file"`
	MaxSizeMB int    `toml:"max_size_mb"`
	MaxFiles  *int   `toml:"max_files"`
}

type rawEngine struct {
	Kind     string         `toml:"kind"`
	Binary   string         `toml:"binary"`
	EvalFile string         `toml:"eval_file"`
	Level    int            `toml:"level"`
	Threads  int            `toml:"threads"`
	Timeout  string         `toml:"timeout"`
	Options  map[string]any `toml:"options"`
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func Load(path string) (*Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("abs app config path: %w", err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read app config: %w", err)
	}
	return Parse(data, filepath.Dir(abs))
}

func Parse(data []byte, baseDir string) (*Config, error) {
	if err := validateTopLevel(data, map[string]bool{
		"setup": true, "log": true, "engines": true,
	}); err != nil {
		return nil, err
	}
	var raw rawConfig
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("app config toml unmarshal: %w", err)
	}
	cfg := &Config{
		Setup:   "missing",
		Engines: map[string]Engine{},
	}
	if raw.Setup != "" {
		switch raw.Setup {
		case "missing", "always", "off":
			cfg.Setup = raw.Setup
		default:
			return nil, fmt.Errorf("app config: setup must be missing, always, or off (got %q)", raw.Setup)
		}
	}
	if raw.Log.File != "" {
		cfg.LogFile = resolvePath(raw.Log.File, baseDir)
		cfg.LogMaxBytes = 100 * 1024 * 1024
		cfg.LogMaxFiles = 5
		if raw.Log.MaxSizeMB < 0 {
			return nil, fmt.Errorf("app config: log.max_size_mb must be >= 0")
		}
		if raw.Log.MaxSizeMB > 0 {
			cfg.LogMaxBytes = int64(raw.Log.MaxSizeMB) * 1024 * 1024
		}
		if raw.Log.MaxFiles != nil {
			if *raw.Log.MaxFiles < 0 {
				return nil, fmt.Errorf("app config: log.max_files must be >= 0")
			}
			cfg.LogMaxFiles = *raw.Log.MaxFiles
		}
	}
	for name, re := range raw.Engines {
		eng, err := parseEngine(name, re, baseDir)
		if err != nil {
			return nil, err
		}
		cfg.Engines[name] = eng
	}
	if len(cfg.Engines) == 0 {
		return nil, errors.New("app config: at least one engine is required")
	}
	return cfg, nil
}

func validateTopLevel(data []byte, allowed map[string]bool) error {
	var top map[string]any
	if err := toml.Unmarshal(data, &top); err != nil {
		return fmt.Errorf("app config toml unmarshal: %w", err)
	}
	var unknown []string
	for key := range top {
		if !allowed[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		return fmt.Errorf("app config: unsupported top-level field(s): %s", strings.Join(unknown, ", "))
	}
	return nil
}

func parseEngine(name string, r rawEngine, baseDir string) (Engine, error) {
	if !nameRE.MatchString(name) {
		return Engine{}, fmt.Errorf("app config: engine name %q must match [A-Za-z0-9_-]+", name)
	}
	kind := EngineKind(r.Kind)
	switch kind {
	case EngineEdax, EngineEgaroucid:
	default:
		return Engine{}, fmt.Errorf("app config: engine %q kind %q is not supported (want edax or egaroucid)", name, r.Kind)
	}
	if r.Binary == "" {
		return Engine{}, fmt.Errorf("app config: engine %q binary is required", name)
	}
	if r.Level < 0 {
		return Engine{}, fmt.Errorf("app config: engine %q level must be >= 0", name)
	}
	timeout := 30 * time.Second
	if r.Timeout != "" {
		d, err := time.ParseDuration(r.Timeout)
		if err != nil {
			return Engine{}, fmt.Errorf("app config: engine %q timeout: %w", name, err)
		}
		if d <= 0 {
			return Engine{}, fmt.Errorf("app config: engine %q timeout must be > 0", name)
		}
		timeout = d
	}
	threads := r.Threads
	if threads <= 0 {
		threads = 1
	}
	opts := map[string]any{}
	for k, v := range r.Options {
		if s, ok := v.(string); ok {
			opts[k] = resolvePath(s, baseDir)
		} else {
			opts[k] = v
		}
	}
	if r.EvalFile != "" {
		opts["eval_file"] = resolvePath(r.EvalFile, baseDir)
	}
	return Engine{
		Name: name, Kind: kind, Binary: resolvePath(r.Binary, baseDir),
		Level: r.Level, Threads: threads, Timeout: timeout, Options: opts,
	}, nil
}

func resolvePath(p, baseDir string) string {
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(baseDir, p))
}
