// Package dataset builds phase-based bitboard value datasets directly from engine self-play.
package dataset

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ikepggthb/reversi-dataset-gen/internal/config"
	"github.com/ikepggthb/reversi-dataset-gen/internal/engine"
	"github.com/pelletier/go-toml/v2"
)

const (
	ModeNormal       = "normal"
	ModeAllPositions = "all_positions"

	FormatBitboard = "rdgbitboard_v1"
	HashDefinition = "canonical_own_opponent_v1"
	MagicBitboard  = "RDGBBVAL1\n"
	RecordSize     = 18
)

var newEngine = engine.New

type Config struct {
	Mode            string
	OutputDir       string
	Phases          []int
	SamplesPerPhase int
	Seed            int64
	SeedSet         bool
	Split           SplitConfig
	Playout         PlayoutConfig
	SelfPlay        SelfPlayConfig
	Window          WindowConfig

	ConfigPath          string
	ConfigSHA256        string
	AppConfigPath       string
	AppConfigSHA256     string
	ConfigEffectiveHash string
	EngineConfig        *config.Config
}

type SplitConfig struct {
	Train float64 `toml:"train"`
	Valid float64 `toml:"valid"`
	Test  float64 `toml:"test"`
}

type PlayoutConfig struct {
	AIProbability         float64
	Dedupe                string
	MaxAttemptsMultiplier int
	AI                    *AIConfig
}

type SelfPlayConfig struct {
	AI      AIConfig
	Workers int
	Retries int
}

type WindowConfig struct {
	Enabled bool  `json:"enabled"`
	Anchors []int `json:"anchors"`
	Size    int   `json:"size"`
}

type AIConfig struct {
	Name    string
	Level   int
	Threads int
	Timeout time.Duration
}

type rawConfig struct {
	Mode            string      `toml:"mode"`
	OutputDir       string      `toml:"output_dir"`
	Phases          string      `toml:"phases"`
	SamplesPerPhase int         `toml:"samples_per_phase"`
	Seed            *int64      `toml:"seed"`
	Split           SplitConfig `toml:"split"`
	Playout         rawPlayout  `toml:"playout"`
	SelfPlay        rawSelfPlay `toml:"self_play"`
	Window          rawWindow   `toml:"window"`
}

type rawPlayout struct {
	AIProbability         float64 `toml:"ai_probability"`
	Dedupe                string  `toml:"dedupe"`
	MaxAttemptsMultiplier int     `toml:"max_attempts_multiplier"`
	AI                    rawAI   `toml:"ai"`
}

type rawSelfPlay struct {
	AI      rawAI `toml:"ai"`
	Workers int   `toml:"workers"`
	Retries int   `toml:"retries"`
}

type rawWindow struct {
	Enabled bool   `toml:"enabled"`
	Anchors string `toml:"anchors"`
	Size    int    `toml:"size"`
}

type rawAI struct {
	Name    string `toml:"name"`
	Level   int    `toml:"level"`
	Threads int    `toml:"threads"`
	Timeout string `toml:"timeout"`
}

func LoadConfig(configPath, appConfigPath string) (Config, error) {
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("config path: %w", err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	appAbs, err := filepath.Abs(appConfigPath)
	if err != nil {
		return Config{}, fmt.Errorf("app config path: %w", err)
	}
	appCfg, err := config.Load(appAbs)
	if err != nil {
		return Config{}, err
	}
	var raw rawConfig
	if err := toml.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("config toml unmarshal: %w", err)
	}
	base := filepath.Dir(abs)
	cfg := Config{
		Mode:            raw.Mode,
		OutputDir:       resolvePath(raw.OutputDir, base),
		SamplesPerPhase: raw.SamplesPerPhase,
		Split:           raw.Split,
		ConfigPath:      abs,
		AppConfigPath:   appAbs,
		EngineConfig:    appCfg,
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeNormal
	}
	if cfg.Mode != ModeNormal && cfg.Mode != ModeAllPositions {
		return Config{}, fmt.Errorf("config: mode must be normal or all_positions (got %q)", cfg.Mode)
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = resolvePath("../datasets", base)
	}
	if cfg.SamplesPerPhase <= 0 && cfg.Mode == ModeNormal {
		return Config{}, errors.New("config: samples_per_phase must be > 0")
	}
	if cfg.SamplesPerPhase < 0 {
		return Config{}, errors.New("config: samples_per_phase must be >= 0")
	}
	phases, err := parsePhases(raw.Phases)
	if err != nil {
		return Config{}, err
	}
	cfg.Phases = phases
	if cfg.Split.Train == 0 && cfg.Split.Valid == 0 && cfg.Split.Test == 0 {
		cfg.Split = SplitConfig{Train: 0.98, Valid: 0.01, Test: 0.01}
	}
	if math.Abs(cfg.Split.Train+cfg.Split.Valid+cfg.Split.Test-1) > 1e-9 {
		return Config{}, fmt.Errorf("config: split ratios must sum to 1")
	}
	if cfg.Split.Train < 0 || cfg.Split.Valid < 0 || cfg.Split.Test < 0 {
		return Config{}, fmt.Errorf("config: split ratios must be >= 0")
	}
	if raw.Seed != nil {
		cfg.Seed = *raw.Seed
		cfg.SeedSet = true
	} else {
		cfg.Seed = time.Now().UnixNano()
	}
	cfg.Playout = PlayoutConfig{
		AIProbability:         raw.Playout.AIProbability,
		Dedupe:                raw.Playout.Dedupe,
		MaxAttemptsMultiplier: raw.Playout.MaxAttemptsMultiplier,
	}
	if cfg.Playout.Dedupe == "" {
		cfg.Playout.Dedupe = "canonical_board"
	}
	if cfg.Playout.Dedupe != "canonical_board" {
		return Config{}, fmt.Errorf("config: playout.dedupe must be canonical_board (got %q)", cfg.Playout.Dedupe)
	}
	if cfg.Playout.MaxAttemptsMultiplier <= 0 {
		cfg.Playout.MaxAttemptsMultiplier = 100
	}
	if cfg.Playout.AIProbability < 0 || cfg.Playout.AIProbability > 1 {
		return Config{}, errors.New("config: playout.ai_probability must be between 0 and 1")
	}
	if cfg.Playout.AIProbability > 0 {
		ai, err := parseAI(raw.Playout.AI, appCfg, "playout.ai", 10*time.Second)
		if err != nil {
			return Config{}, err
		}
		cfg.Playout.AI = &ai
	}
	selfAI, err := parseAI(raw.SelfPlay.AI, appCfg, "self_play.ai", 300*time.Second)
	if err != nil {
		return Config{}, err
	}
	workers := raw.SelfPlay.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
		if workers < 1 {
			workers = 1
		}
	}
	retries := raw.SelfPlay.Retries
	if retries <= 0 {
		retries = 3
	}
	cfg.SelfPlay = SelfPlayConfig{AI: selfAI, Workers: workers, Retries: retries}
	if raw.Window.Enabled {
		if strings.TrimSpace(raw.Window.Anchors) != "" {
			return Config{}, errors.New("config: window.anchors is no longer supported; use window.size")
		}
		if raw.Window.Size <= 0 {
			return Config{}, errors.New("config: window.size must be > 0")
		}
		if cfg.Mode != ModeNormal {
			return Config{}, errors.New("config: window is only supported in normal mode")
		}
		anchors := deriveWindowAnchors(cfg.Phases, raw.Window.Size)
		cfg.Window = WindowConfig{Enabled: true, Anchors: anchors, Size: raw.Window.Size}
		if len(windowTargetPhases(cfg)) == 0 {
			return Config{}, errors.New("config: window ranges do not overlap phases")
		}
		if missing := phasesOutsideWindow(cfg.Phases, cfg.Window); len(missing) > 0 {
			return Config{}, fmt.Errorf("config: phases not covered by window ranges: %s", formatPhaseList(missing))
		}
	}
	cfg.ConfigSHA256, err = fileSHA256(abs)
	if err != nil {
		return Config{}, err
	}
	cfg.AppConfigSHA256, err = fileSHA256(appAbs)
	if err != nil {
		return Config{}, err
	}
	cfg.ConfigEffectiveHash = effectiveHash(cfg)
	return cfg, nil
}

func parseAI(raw rawAI, appCfg *config.Config, section string, defaultTimeout time.Duration) (AIConfig, error) {
	if raw.Name == "" {
		return AIConfig{}, fmt.Errorf("config: %s.name is required", section)
	}
	if _, ok := appCfg.Engines[raw.Name]; !ok {
		return AIConfig{}, fmt.Errorf("config: %s.name %q is not defined in app config", section, raw.Name)
	}
	if raw.Level < 0 {
		return AIConfig{}, fmt.Errorf("config: %s.level must be >= 0", section)
	}
	threads := raw.Threads
	if threads <= 0 {
		threads = 1
	}
	timeout := defaultTimeout
	if raw.Timeout != "" {
		d, err := time.ParseDuration(raw.Timeout)
		if err != nil {
			return AIConfig{}, fmt.Errorf("config: %s.timeout: %w", section, err)
		}
		if d <= 0 {
			return AIConfig{}, fmt.Errorf("config: %s.timeout must be > 0", section)
		}
		timeout = d
	}
	return AIConfig{Name: raw.Name, Level: raw.Level, Threads: threads, Timeout: timeout}, nil
}

func parsePhases(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("config: phases is required")
	}
	var phases []int
	if strings.Contains(s, "..") {
		parts := strings.Split(s, "..")
		if len(parts) != 2 {
			return nil, fmt.Errorf("config: invalid phases %q", s)
		}
		a, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return nil, err
		}
		b, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, err
		}
		if a > b {
			return nil, fmt.Errorf("config: phase range must be ascending")
		}
		for i := a; i <= b; i++ {
			phases = append(phases, i)
		}
	} else {
		for _, p := range strings.Split(s, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				return nil, err
			}
			phases = append(phases, n)
		}
	}
	seen := map[int]bool{}
	for _, p := range phases {
		if p < 1 || p > 60 {
			return nil, fmt.Errorf("config: phase %d out of range 1..60", p)
		}
		if seen[p] {
			return nil, fmt.Errorf("config: duplicate phase %d", p)
		}
		seen[p] = true
	}
	return phases, nil
}

func effectiveHash(cfg Config) string {
	type eff struct {
		Mode            string         `json:"mode"`
		Phases          []int          `json:"phases"`
		Seed            int64          `json:"seed"`
		Split           SplitConfig    `json:"split"`
		Playout         PlayoutConfig  `json:"playout"`
		SelfPlay        SelfPlayConfig `json:"self_play"`
		Window          WindowConfig   `json:"window"`
		AppConfigSHA256 string         `json:"app_config_sha256"`
		HashDefinition  string         `json:"hash_definition"`
	}
	b, _ := json.Marshal(eff{cfg.Mode, cfg.Phases, cfg.Seed, cfg.Split, cfg.Playout, cfg.SelfPlay, cfg.Window, cfg.AppConfigSHA256, HashDefinition})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
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
