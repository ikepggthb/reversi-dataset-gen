package identity

import (
	"testing"

	"github.com/ikepggthb/reversi-dataset-gen/internal/config"
)

func TestEvalFilePath(t *testing.T) {
	cases := []struct {
		name string
		eng  config.Engine
		want string
	}{
		{
			name: "explicit",
			eng: config.Engine{
				Kind:    config.EngineEdax,
				Binary:  "/x/bin/lEdax",
				Options: map[string]any{"eval_file": "/custom/eval.dat"},
			},
			want: "/custom/eval.dat",
		},
		{
			name: "edax default",
			eng:  config.Engine{Kind: config.EngineEdax, Binary: "/x/bin/lEdax"},
			want: "/x/bin/data/eval.dat",
		},
		{
			name: "egaroucid default",
			eng:  config.Engine{Kind: config.EngineEgaroucid, Binary: "/x/bin/Egaroucid_for_Console.out"},
			want: "/x/bin/resources/eval.egev2",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := EvalFilePath(c.eng); got != c.want {
				t.Fatalf("EvalFilePath = %q, want %q", got, c.want)
			}
		})
	}
}
