package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validAppTOML = `
setup = "missing"

[log]
file = "logs/rdg.log"
max_size_mb = 42
max_files = 3

[engines.edax]
kind = "edax"
binary = "bin/lEdax"
eval_file = "bin/data/eval.dat"

[engines.egaroucid]
kind = "egaroucid"
binary = "bin/Egaroucid_for_Console.out"
eval_file = "bin/resources/eval.egev2"
`

func TestParseAppConfig(t *testing.T) {
	cfg, err := Parse([]byte(validAppTOML), "/base")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Setup != "missing" {
		t.Fatalf("setup = %q", cfg.Setup)
	}
	if got := cfg.LogFile; got != "/base/logs/rdg.log" {
		t.Fatalf("log file = %q", got)
	}
	if got := cfg.LogMaxBytes; got != 42*1024*1024 {
		t.Fatalf("log max bytes = %d", got)
	}
	if got := cfg.LogMaxFiles; got != 3 {
		t.Fatalf("log max files = %d", got)
	}
	if got := cfg.Engines["edax"].Binary; got != "/base/bin/lEdax" {
		t.Fatalf("edax binary = %q", got)
	}
	if got := cfg.Engines["edax"].Options["eval_file"]; got != "/base/bin/data/eval.dat" {
		t.Fatalf("edax eval_file = %v", got)
	}
	if names := strings.Join(cfg.UsedEngineNames(), ","); names != "edax,egaroucid" {
		t.Fatalf("UsedEngineNames = %q", names)
	}
}

func TestLoadAppConfigResolvesRelativePaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	if err := os.WriteFile(path, []byte(validAppTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Engines["edax"].Binary; got != filepath.Join(dir, "bin/lEdax") {
		t.Fatalf("engine binary = %q", got)
	}
}

func TestParseAppConfigErrors(t *testing.T) {
	cases := []struct {
		name    string
		toml    string
		wantSub string
	}{
		{
			name:    "missing engines",
			toml:    `setup = "missing"`,
			wantSub: "at least one engine",
		},
		{
			name: "profiles rejected",
			toml: `
[engines.edax]
kind = "edax"
binary = "x"
[profiles.fast]
level = 1
`,
			wantSub: "unsupported top-level",
		},
		{
			name: "unknown engine kind",
			toml: `
[engines.bad]
kind = "unknown"
binary = "x"
`,
			wantSub: "kind",
		},
		{
			name: "invalid setup",
			toml: `
setup = "sometimes"
[engines.edax]
kind = "edax"
binary = "x"
`,
			wantSub: "setup",
		},
		{
			name: "legacy singular engine is rejected",
			toml: `
[engine]
kind = "edax"
binary = "x"
`,
			wantSub: "unsupported top-level",
		},
		{
			name: "negative log max size rejected",
			toml: `
[log]
file = "x.log"
max_size_mb = -1
[engines.edax]
kind = "edax"
binary = "x"
`,
			wantSub: "log.max_size_mb",
		},
		{
			name: "negative log max files rejected",
			toml: `
[log]
file = "x.log"
max_files = -1
[engines.edax]
kind = "edax"
binary = "x"
`,
			wantSub: "log.max_files",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.toml), "/base")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Fatalf("error %q should contain %q", err.Error(), c.wantSub)
			}
		})
	}
}
