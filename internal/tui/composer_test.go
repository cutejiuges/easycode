package tui

import (
	"testing"
)

func composerWithText(t *testing.T, text string) composer {
	t.Helper()
	c := newComposer()
	if err := c.replace(0, 0, text); err != nil {
		t.Fatal(err)
	}
	return c
}
