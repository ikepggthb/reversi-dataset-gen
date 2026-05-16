package dataset

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ikepggthb/reversi-dataset-gen/internal/board"
	"github.com/ikepggthb/reversi-dataset-gen/internal/config"
)

type phaseStats struct {
	Schema                string             `json:"schema"`
	Mode                  string             `json:"mode"`
	Phase                 int                `json:"phase"`
	StartedAt             string             `json:"started_at"`
	FinishedAt            string             `json:"finished_at"`
	ElapsedSec            float64            `json:"elapsed_seconds"`
	TargetSamples         int                `json:"target_samples"`
	EmittedSamples        int                `json:"emitted_samples"`
	Attempts              int64              `json:"attempts,omitempty"`
	DuplicatePositions    int64              `json:"duplicate_positions"`
	DuplicateRate         float64            `json:"duplicate_rate"`
	FailedAttempts        int64              `json:"failed_attempts,omitempty"`
	Playout               *playoutStats      `json:"playout,omitempty"`
	Enumeration           *enumerationStats  `json:"enumeration,omitempty"`
	Evaluation            *evaluationStats   `json:"evaluation,omitempty"`
	SelfPlay              *selfPlayStats     `json:"self_play,omitempty"`
	Window                *windowPhaseStats  `json:"window,omitempty"`
	Value                 valueStats         `json:"value"`
	Split                 map[string]int64   `json:"split"`
	Speed                 map[string]float64 `json:"speed"`
	EnumerationElapsedSec float64            `json:"-"`
	EvaluationElapsedSec  float64            `json:"-"`
}

type playoutStats struct {
	RandomMoves   int64   `json:"random_moves"`
	EngineMoves   int64   `json:"engine_moves"`
	AIProbability float64 `json:"ai_probability"`
	PassCount     int64   `json:"pass_count"`
}

type selfPlayStats struct {
	Games          int64 `json:"games"`
	Moves          int64 `json:"moves"`
	PassCount      int64 `json:"pass_count"`
	EngineTimeouts int64 `json:"engine_timeouts"`
	EngineErrors   int64 `json:"engine_errors"`
}

type enumerationStats struct {
	Nodes         int     `json:"nodes"`
	Leaf          int     `json:"leaf"`
	Unique        int     `json:"unique"`
	Duplicates    int     `json:"duplicates"`
	Pass          int     `json:"pass"`
	GameOver      int     `json:"game_over"`
	DuplicateRate float64 `json:"duplicate_rate"`
}

type evaluationStats struct {
	Target         int   `json:"target"`
	Evaluated      int64 `json:"evaluated"`
	Failed         int64 `json:"failed"`
	SelfPlayGames  int64 `json:"self_play_games"`
	Moves          int64 `json:"moves"`
	PassCount      int64 `json:"pass_count"`
	EngineTimeouts int64 `json:"engine_timeouts"`
	EngineErrors   int64 `json:"engine_errors"`
}

type windowPhaseStats struct {
	ExtractedSamples   int64 `json:"extracted_samples"`
	DuplicateSamples   int64 `json:"duplicate_samples"`
	FailedTrajectories int64 `json:"failed_trajectories"`
	EngineTimeouts     int64 `json:"engine_timeouts"`
	EngineErrors       int64 `json:"engine_errors"`
}

type valueStats struct {
	Min       int            `json:"min"`
	Max       int            `json:"max"`
	Mean      float64        `json:"mean"`
	Histogram map[string]int `json:"histogram"`
}

type metadata struct {
	Schema              string             `json:"schema"`
	Mode                string             `json:"mode"`
	Generation          string             `json:"generation,omitempty"`
	Phase               int                `json:"phase"`
	Format              string             `json:"format"`
	RecordMagic         string             `json:"record_magic"`
	RecordSizeBytes     int                `json:"record_size_bytes"`
	RecordSpec          string             `json:"record_spec"`
	HashDefinition      string             `json:"hash_definition"`
	ConfigPath          string             `json:"config_path"`
	ConfigSHA256        string             `json:"config_sha256"`
	ConfigEffectiveHash string             `json:"config_effective_hash"`
	AppConfigPath       string             `json:"app_config_path"`
	AppConfigSHA256     string             `json:"app_config_sha256"`
	Engine              engineMetadata     `json:"engine"`
	EngineIdentity      config.RunIdentity `json:"engine_identity"`
	GeneratorCommit     string             `json:"generator_commit,omitempty"`
	GeneratorDirty      bool               `json:"generator_dirty"`
	GeneratorDirtyKnown bool               `json:"generator_dirty_known"`
	Split               SplitConfig        `json:"split"`
	Seed                int64              `json:"seed"`
	SamplesPerPhase     int                `json:"samples_per_phase"`
	Window              WindowConfig       `json:"window,omitempty"`
	OutputFiles         map[string]string  `json:"output_files"`
	GeneratedAt         string             `json:"generated_at"`
}

type engineMetadata struct {
	Playout  *AIConfig `json:"playout,omitempty"`
	SelfPlay AIConfig  `json:"self_play"`
}

type summary struct {
	Schema       string           `json:"schema"`
	Mode         string           `json:"mode"`
	Generation   string           `json:"generation,omitempty"`
	StartedAt    string           `json:"started_at"`
	FinishedAt   string           `json:"finished_at"`
	ElapsedSec   float64          `json:"elapsed_seconds"`
	Phases       []int            `json:"phases"`
	TotalSamples int64            `json:"total_samples"`
	Split        map[string]int64 `json:"split"`
	PhaseStats   []phaseSummary   `json:"phase_stats"`
}

type phaseSummary struct {
	Phase              int     `json:"phase"`
	EmittedSamples     int     `json:"emitted_samples"`
	FailedAttempts     int64   `json:"failed_attempts"`
	DuplicatePositions int64   `json:"duplicate_positions"`
	DuplicateRate      float64 `json:"duplicate_rate"`
}

type metrics struct {
	playoutRandom int64
	playoutEngine int64
	playoutPass   int64
	selfGames     int64
	selfMoves     int64
	selfPass      int64
	timeouts      int64
	errors        int64
	failed        int64
}

func buildStats(cfg Config, phase, target int, emitted, attempts, dup int64, m metrics, vals *valueAgg, splits map[string]int64) phaseStats {
	st := phaseStats{
		Schema: "rdg_phase_stats_v1", Mode: cfg.Mode, Phase: phase,
		TargetSamples: target, EmittedSamples: int(emitted),
		Attempts: attempts, DuplicatePositions: dup,
		DuplicateRate: ratio(dup, attempts), FailedAttempts: m.failed,
		Playout: &playoutStats{
			RandomMoves: m.playoutRandom, EngineMoves: m.playoutEngine,
			AIProbability: cfg.Playout.AIProbability, PassCount: m.playoutPass,
		},
		SelfPlay: &selfPlayStats{
			Games: m.selfGames, Moves: m.selfMoves, PassCount: m.selfPass,
			EngineTimeouts: m.timeouts, EngineErrors: m.errors,
		},
		Value: vals.stats(), Split: copyCounts(splits),
		Speed: map[string]float64{},
	}
	return st
}

func fillSpeed(st *phaseStats) {
	if st.ElapsedSec <= 0 {
		st.Speed = map[string]float64{}
		return
	}
	if st.Speed == nil {
		st.Speed = map[string]float64{}
	}
	if st.Schema == "rdg_all_positions_phase_stats_v1" {
		if st.Enumeration != nil {
			elapsed := st.EnumerationElapsedSec
			if elapsed <= 0 {
				elapsed = st.ElapsedSec
			}
			st.Speed["enumeration_nodes_per_second"] = float64(st.Enumeration.Nodes) / elapsed
		}
		elapsed := st.EvaluationElapsedSec
		if elapsed <= 0 {
			elapsed = st.ElapsedSec
		}
		st.Speed["evaluation_positions_per_second"] = float64(st.EmittedSamples) / elapsed
		return
	}
	st.Speed["samples_per_second"] = float64(st.EmittedSamples) / st.ElapsedSec
	if st.SelfPlay != nil {
		st.Speed["self_play_games_per_second"] = float64(st.SelfPlay.Games) / st.ElapsedSec
	}
}

func buildMetadata(cfg Config, phase int) metadata {
	meta := buildRunState(cfg, phase)
	meta.Schema = "rdg_phase_metadata_v1"
	meta.GeneratedAt = time.Now().Format(time.RFC3339)
	return meta
}

func buildRunState(cfg Config, phase int) metadata {
	generation := "phase"
	if cfg.Window.Enabled {
		generation = "window"
	}
	return metadata{
		Schema: "rdg_phase_run_state_v1", Mode: cfg.Mode, Generation: generation, Phase: phase,
		Format: FormatBitboard, RecordMagic: MagicBitboard, RecordSizeBytes: RecordSize,
		RecordSpec:     "u64 own_bits little-endian, u64 opponent_bits little-endian, i16 value little-endian; side-to-move perspective; bit0=a1, bit63=h8",
		HashDefinition: HashDefinition,
		ConfigPath:     cfg.ConfigPath, ConfigSHA256: cfg.ConfigSHA256, ConfigEffectiveHash: cfg.ConfigEffectiveHash,
		AppConfigPath: cfg.AppConfigPath, AppConfigSHA256: cfg.AppConfigSHA256,
		Engine:              engineMetadata{Playout: cfg.Playout.AI, SelfPlay: cfg.SelfPlay.AI},
		EngineIdentity:      cfg.EngineConfig.RunIdentity,
		GeneratorCommit:     cfg.EngineConfig.RunIdentity.GeneratorCommit,
		GeneratorDirty:      cfg.EngineConfig.RunIdentity.GeneratorDirty,
		GeneratorDirtyKnown: cfg.EngineConfig.RunIdentity.GeneratorDirtyKnown,
		Split:               cfg.Split, Seed: cfg.Seed, SamplesPerPhase: cfg.SamplesPerPhase, Window: cfg.Window,
		OutputFiles: map[string]string{"train": "train.rd", "valid": "valid.rd", "test": "test.rd"},
		GeneratedAt: time.Now().Format(time.RFC3339),
	}
}

type valueAgg struct {
	count int
	sum   int
	min   int
	max   int
	hist  map[string]int
}

func newValueAgg() *valueAgg { return &valueAgg{min: 999, max: -999, hist: map[string]int{}} }
func (v *valueAgg) add(x int) {
	v.count++
	v.sum += x
	if x < v.min {
		v.min = x
	}
	if x > v.max {
		v.max = x
	}
	v.hist[strconv.Itoa(x)]++
}
func (v *valueAgg) stats() valueStats {
	if v.count == 0 {
		return valueStats{Histogram: map[string]int{}}
	}
	return valueStats{Min: v.min, Max: v.max, Mean: float64(v.sum) / float64(v.count), Histogram: v.hist}
}

func valueAggFromStats(st valueStats) *valueAgg {
	v := newValueAgg()
	for key, count := range st.Histogram {
		x, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		for i := 0; i < count; i++ {
			v.add(x)
		}
	}
	return v
}

func metricsFromStats(st phaseStats) metrics {
	m := metrics{failed: st.FailedAttempts}
	if st.Playout != nil {
		m.playoutRandom = st.Playout.RandomMoves
		m.playoutEngine = st.Playout.EngineMoves
		m.playoutPass = st.Playout.PassCount
	}
	if st.SelfPlay != nil {
		m.selfGames = st.SelfPlay.Games
		m.selfMoves = st.SelfPlay.Moves
		m.selfPass = st.SelfPlay.PassCount
		m.timeouts = st.SelfPlay.EngineTimeouts
		m.errors = st.SelfPlay.EngineErrors
	}
	if st.Window != nil {
		m.timeouts = st.Window.EngineTimeouts
		m.errors = st.Window.EngineErrors
		m.failed = st.Window.FailedTrajectories
	}
	return m
}

func resolveEngine(cfg Config, ai AIConfig) config.Engine {
	eng := cfg.EngineConfig.Engines[ai.Name]
	eng.Level = ai.Level
	eng.Threads = ai.Threads
	eng.Timeout = ai.Timeout
	return eng
}

func phaseOf(b board.Board) int {
	black, white, _ := b.DiscCounts()
	return black + white - 4
}

func chooseSplit(hash string, split SplitConfig) string {
	sum := sha256.Sum256([]byte(hash))
	x := binary.BigEndian.Uint64(sum[:8])
	f := float64(x) / float64(math.MaxUint64)
	if f < split.Train {
		return "train"
	}
	if f < split.Train+split.Valid {
		return "valid"
	}
	return "test"
}

func loadMetadata(path string) (metadata, bool, error) {
	var meta metadata
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return meta, false, nil
	}
	if err != nil {
		return meta, false, err
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return meta, false, err
	}
	return meta, true, nil
}

func loadResumeState(dir string) (metadata, bool, error) {
	state, ok, err := loadMetadata(filepath.Join(dir, "run_state.json"))
	if err != nil || ok {
		return state, ok, err
	}
	return loadMetadata(filepath.Join(dir, "metadata.json"))
}

func checkResumeMetadata(meta metadata, cfg Config, phase int) error {
	if meta.HashDefinition != HashDefinition || meta.ConfigEffectiveHash != cfg.ConfigEffectiveHash || meta.Phase != phase || meta.Mode != cfg.Mode {
		return fmt.Errorf("phase_%02d: existing metadata is incompatible with current config; use --resume --repair to rebuild this phase or --force to rebuild all", phase)
	}
	return nil
}

func readStats(path string) (phaseStats, error) {
	var st phaseStats
	data, err := os.ReadFile(path)
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(data, &st)
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func copyCounts(in map[string]int64) map[string]int64 {
	out := map[string]int64{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func ratio(a, b int64) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}
func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
func addMetrics(dst *metrics, src metrics) {
	dst.playoutRandom += src.playoutRandom
	dst.playoutEngine += src.playoutEngine
	dst.playoutPass += src.playoutPass
	dst.selfGames += src.selfGames
	dst.selfMoves += src.selfMoves
	dst.selfPass += src.selfPass
	dst.timeouts += src.timeouts
	dst.errors += src.errors
	dst.failed += src.failed
}
