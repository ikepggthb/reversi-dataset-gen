package engine

import "testing"

func TestParseGTPResponseLine(t *testing.T) {
	cases := []struct {
		line   string
		prefix string
		want   string
		ok     bool
	}{
		{"=1", "=1", "", true},
		{"=1 F5", "=1", "F5", true},
		{"=12 PASS", "=12", "PASS", true},
		{"?3 illegal move", "?3", "illegal move", true},
		{"=10 F5", "=1", "", false},
		{"log line", "=1", "", false},
	}
	for _, c := range cases {
		got, ok := parseGTPResponseLine(c.line, c.prefix)
		if ok != c.ok || got != c.want {
			t.Fatalf("parseGTPResponseLine(%q, %q) = %q, %v; want %q, %v",
				c.line, c.prefix, got, ok, c.want, c.ok)
		}
	}
}
