package app

import "testing"

func TestMarkdownColorSelection(t *testing.T) {
	for _, test := range []struct {
		noColor, term  string
		terminal, want bool
	}{
		{"", "xterm-256color", true, true}, {"1", "xterm", true, false}, {"", "dumb", true, false}, {"", "xterm", false, false},
	} {
		if got := markdownColorEnabled(test.noColor, test.term, test.terminal); got != test.want {
			t.Fatalf("color policy %+v got %v", test, got)
		}
	}
}
