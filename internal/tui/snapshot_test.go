package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/protocol"
)

func snapshotIdleModel(t *testing.T) Model {
	t.Helper()
	return NewModel(context.Background(), "test", &fakeChatSession{})
}

func snapshotStreamingModel(t *testing.T) Model {
	t.Helper()
	model := updateModel(t, snapshotIdleModel(t), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pending := updated.(Model)
	accepted := updateModel(t, pending, testAdmissionMessage(pending, cmd))
	return testCommitAcceptedInput(t, accepted)
}

func snapshotCompletedModel(t *testing.T) Model {
	t.Helper()
	model := snapshotStreamingModel(t)
	delta, err := protocol.NewAssistantTextDelta("hi")
	if err != nil {
		t.Fatalf("new delta: %v", err)
	}
	model = updateModel(t, model, RuntimeEventMsg{Event: delta})
	return updateModel(t, model, RuntimeEventMsg{Event: mustRuntimeCompleted(t)})
}
