package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/fault"
	"easycode/internal/protocol"
)

func TestModelSnapshotsAtFixedTerminalSize(t *testing.T) {
	tests := map[string]Model{
		"idle":      snapshotIdleModel(t),
		"draft":     snapshotDraftModel(t),
		"streaming": snapshotStreamingModel(t),
		"completed": snapshotCompletedModel(t),
		"cancelled": snapshotCancelledModel(t),
		"failed":    snapshotFailedModel(t),
	}

	var allSnapshots strings.Builder
	for name, model := range tests {
		t.Run(name, func(t *testing.T) {
			model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
			got := model.View()
			want, err := os.ReadFile(filepath.Join("testdata", name+".golden"))
			if err != nil {
				t.Fatalf("read snapshot: %v", err)
			}
			if got != string(want) {
				t.Fatalf("snapshot mismatch:\n--- got ---\n%s--- want ---\n%s", got, want)
			}
			allSnapshots.WriteString(got)
		})
	}

	rendered := allSnapshots.String()
	for _, forbidden := range []string{"top-secret", "Authorization", "Bearer ", "api_key", `"input"`} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("snapshot contains sensitive request data %q", forbidden)
		}
	}
}

func snapshotIdleModel(t *testing.T) Model {
	t.Helper()
	return NewModel("test", &fakeChatSession{})
}

func snapshotDraftModel(t *testing.T) Model {
	t.Helper()
	return updateModel(t, snapshotIdleModel(t), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
}

func snapshotStreamingModel(t *testing.T) Model {
	t.Helper()
	model := updateModel(t, snapshotIdleModel(t), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return updated.(Model)
}

func snapshotCompletedModel(t *testing.T) Model {
	t.Helper()
	model := snapshotStreamingModel(t)
	delta, err := protocol.NewAssistantTextDelta("hi")
	if err != nil {
		t.Fatalf("new delta: %v", err)
	}
	model = updateModel(t, model, RuntimeEventMsg{Event: delta})
	return updateModel(t, model, RuntimeEventMsg{Event: protocol.NewEvent(protocol.EventTurnCompleted)})
}

func snapshotCancelledModel(t *testing.T) Model {
	t.Helper()
	model := snapshotStreamingModel(t)
	delta, err := protocol.NewAssistantTextDelta("par")
	if err != nil {
		t.Fatalf("new delta: %v", err)
	}
	model = updateModel(t, model, RuntimeEventMsg{Event: delta})
	failure, err := protocol.NewTurnFailed(string(fault.CodeUserCancelled), "turn was cancelled", true)
	if err != nil {
		t.Fatalf("new cancellation: %v", err)
	}
	return updateModel(t, model, RuntimeEventMsg{Event: failure})
}

func snapshotFailedModel(t *testing.T) Model {
	t.Helper()
	model := snapshotStreamingModel(t)
	model.transcript = append([]transcriptMessage{{role: "User", text: "previous"}, {role: "Assistant", text: "kept"}}, model.transcript...)
	failure, err := protocol.NewTurnFailed(string(fault.CodeProviderRequest), "provider request failed", false)
	if err != nil {
		t.Fatalf("new failure: %v", err)
	}
	return updateModel(t, model, RuntimeEventMsg{Event: failure})
}
