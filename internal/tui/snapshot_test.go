package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/domain"
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
		"resumed":   snapshotResumedModel(t),
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

func TestApprovalOverlaySnapshots(t *testing.T) {
	for _, test := range []struct {
		name   string
		width  int
		height int
		mutate func(*Model)
	}{
		{name: "approval", width: 80, height: 24},
		{name: "approval_small", width: 34, height: 12},
		{name: "approval_feedback", width: 80, height: 24, mutate: func(model *Model) {
			model.approval.editing = true
			model.approval.feedback = []rune("请保留旧名")
		}},
		{name: "approval_submitted", width: 80, height: 24, mutate: func(model *Model) {
			model.approval.submitted = true
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := snapshotStreamingModel(t)
			approval := mustApprovalRequested(t)
			approval.TurnID = model.activeTurnID
			model = updateModel(t, model, RuntimeEventMsg{Event: approval})
			model = updateModel(t, model, tea.WindowSizeMsg{Width: test.width, Height: test.height})
			if test.mutate != nil {
				test.mutate(&model)
			}
			got := model.View()
			want, err := os.ReadFile(filepath.Join("testdata", test.name+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("snapshot mismatch:\n--- got ---\n%s--- want ---\n%s", got, want)
			}
		})
	}
}

func snapshotResumedModel(t *testing.T) Model {
	t.Helper()

	model := NewModel(context.Background(), "test", &fakeChatSession{})
	turn := domain.TurnID("018f1d8a-7b5c-7def-8234-456789abcdef")
	items := []protocol.TranscriptListItem{}
	for i, text := range []string{"second answer", "second question", "first answer", "first question"} {
		item := protocol.TranscriptListItem{Cursor: protocol.TranscriptCursor{Sequence: uint64(4 - i)}, Kind: domain.TranscriptAssistant, TurnID: turn, Text: text}
		if i%2 == 1 {
			item.Kind = domain.TranscriptUser
			item.InputID = domain.InputID("01900000-0000-7000-8000-000000000001")
		}
		items = append(items, item)
	}
	return updateModel(t, model, mainTranscriptReply{page: protocol.TranscriptListPage{Items: items}})

}

func snapshotIdleModel(t *testing.T) Model {
	t.Helper()
	return NewModel(context.Background(), "test", &fakeChatSession{})
}

func snapshotDraftModel(t *testing.T) Model {
	t.Helper()
	return updateModel(t, snapshotIdleModel(t), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
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
