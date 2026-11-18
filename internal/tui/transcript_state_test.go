package tui

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type mainTestQueries struct{ page protocol.TranscriptListPage }

func (q *mainTestQueries) ListTranscript(context.Context, protocol.TranscriptListQuery) (protocol.TranscriptListPage, error) {
	return q.page, nil
}
func (q *mainTestQueries) ReadTranscriptBody(context.Context, protocol.TranscriptBodyQuery) (protocol.TranscriptBodyPage, error) {
	return protocol.TranscriptBodyPage{}, nil
}
func (q *mainTestQueries) ReadToolOutput(context.Context, protocol.ToolOutputQuery) (protocol.ToolOutputPage, error) {
	return protocol.ToolOutputPage{}, nil
}

func TestMainTranscriptReplacesProvisionalExactlyOnceAndRejectsLateReplies(t *testing.T) {
	turn := domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcdef")
	thread := domain.ThreadID(turn)
	q := &mainTestQueries{page: protocol.TranscriptListPage{Items: []protocol.TranscriptListItem{{Cursor: protocol.TranscriptCursor{Sequence: 3}, Kind: domain.TranscriptAssistant, TurnID: turn, Text: "committed answer"}}}}
	model := NewModel(context.Background(), "test", &fakeChatSession{}).WithTranscriptQueries(thread, q)
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.activeTurnID = turn
	model.state = stateStreaming
	delta, _ := protocol.NewAssistantTextDelta("committed answer")
	delta.TurnID = turn
	model = updateModel(t, model, RuntimeEventMsg{Event: delta})
	if !strings.Contains(model.View(), "committed answer") {
		t.Fatal("provisional missing")
	}
	event, _ := protocol.NewInputTranscriptCommitted(3)
	event.TurnID = turn
	next, cmd := model.Update(RuntimeEventMsg{Event: event})
	model = next.(Model)
	reply := cmd()
	delta, _ = protocol.NewAssistantTextDelta("next provisional")
	delta.TurnID = turn
	model = updateModel(t, model, RuntimeEventMsg{Event: delta})
	model = updateModel(t, model, reply)
	view := model.View()
	if strings.Count(view, "committed answer") != 1 || strings.Count(view, "next provisional") != 1 {
		t.Fatalf("sample replacement: %s", view)
	}
	stale := mainTranscriptReply{generation: model.main.generation - 1, page: protocol.TranscriptListPage{}}
	model = updateModel(t, model, stale)
	if model.View() != view {
		t.Fatal("late reply replaced snapshot")
	}
	failed, _ := protocol.NewTurnFailed("turn_failed", "failed", false)
	failed.TurnID = turn
	model = updateModel(t, model, RuntimeEventMsg{Event: failed})
	if !strings.Contains(model.View(), "Assistant (unfinished)") || !strings.Contains(model.View(), "next provisional") {
		t.Fatal("unfinished text claimed committed")
	}
	restored := NewModel(context.Background(), "test", &fakeChatSession{}).WithTranscriptQueries(thread, q)
	restored = updateModel(t, restored, restored.initialTranscriptQuery()())
	if strings.Contains(restored.View(), "next provisional") || !strings.Contains(restored.View(), "committed answer") {
		t.Fatal("restore included uncommitted text")
	}
}
