package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCompletionLocalDispatchAndInvalidArguments(t *testing.T) {
	s := &cacheFixtureSession{}
	m := NewModel(context.Background(), "test", s)
	m.composer = composerWithText(t, "/cache")
	m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.pending == nil || len(s.submits) != 0 {
		t.Fatal("local command escaped its dispatch owner")
	}
	cmd()
	if len(s.queries) != 1 {
		t.Fatal("cache was not queried exactly once")
	}
	for _, command := range []string{"/context bad", "/cache bad", "/skills bad", "/hooks bad", "/plugins bad"} {
		m := completionModel(t, command)
		m.composer.moveTo(strings.IndexByte(command, ' '))
		m.prepareCompletion()
		m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil || !strings.Contains(m.errorSummary, "does not accept arguments") || m.composer.text != command {
			t.Fatalf("argument validation lost: %s %s", command, m.errorSummary)
		}
	}
}
