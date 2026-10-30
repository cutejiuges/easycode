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
	event, _ := protocol.NewTranscriptCommitted(3)
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
	if !strings.Contains(model.View(), "Assistant (unfinished): next provisional") {
		t.Fatal("unfinished text claimed committed")
	}
	restored := NewModel(context.Background(), "test", &fakeChatSession{}).WithTranscriptQueries(thread, q)
	restored = updateModel(t, restored, restored.initialTranscriptQuery()())
	if strings.Contains(restored.View(), "next provisional") || !strings.Contains(restored.View(), "committed answer") {
		t.Fatal("restore included uncommitted text")
	}
}

func TestTranscriptGroupingAndShellThreeLinePresentation(t *testing.T) {
	read := func(target, status string) transcriptMessage {
		return transcriptMessage{activity: &protocol.ToolActivityPayload{Tool: protocol.ToolRead, Target: target, Summary: target, Status: status}}
	}
	search := transcriptMessage{activity: &protocol.ToolActivityPayload{Tool: protocol.ToolGrep, Summary: "needle", Status: "success"}}
	shell := transcriptMessage{activity: &protocol.ToolActivityPayload{Tool: protocol.ToolShell, Summary: "run", Status: "success", ExitKnown: true, ExitCode: 7}, preview: "one\ntwo\nthree\nfour\nfive"}
	messages := []transcriptMessage{read("a", "success"), read("a", "success"), search, read("b", "success"), read("c", "error"), read("d", "success"), {role: "Assistant", text: "break"}, search, shell}
	rendered := strings.Join(renderTranscript(messages, 80), "\n")
	for _, want := range []string{"Read 2 files · 1 searches", "Read 1 files · 0 searches", "Read 0 files · 1 searches", "Read(c) · error", "exit 7", "one\n  two\n  three", "Ctrl+O for details"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("missing %q in %s", want, rendered)
		}
	}
	if strings.Contains(rendered, "four") || strings.Contains(rendered, "five") {
		t.Fatal("shell exceeded three lines")
	}
}

func TestRecallKeepsFullHistoryIndependentOfTranscriptPage(t *testing.T) {
	history := domain.SemanticHistoryView{}
	for i := 0; i < 120; i++ {
		history.Turns = append(history.Turns, domain.SemanticTurn{Messages: []domain.SemanticMessage{{Role: domain.SemanticUser, Text: strings.Repeat("界", 600) + strings.Repeat("x", i) + "\r\nlast"}}})
	}
	model := NewModel(context.Background(), "test", nil).WithInputHistory(history)
	if len(model.composer.history.entries) != 100 || model.composer.history.entries[99] != strings.ReplaceAll(history.Turns[119].Messages[0].Text, "\r\n", "\n") {
		t.Fatal("recall lost full bounded input history")
	}
	before := model.composer.history.entries[99]
	model = updateModel(t, model, mainTranscriptReply{generation: model.main.generation, page: protocol.TranscriptListPage{}})
	if model.composer.history.entries[99] != before {
		t.Fatal("transcript replaced recall history")
	}
}

func TestSavingFailureBreaksNormalSearchGrouping(t *testing.T) {
	for _, status := range []string{"partial", "unavailable"} {
		message := transcriptMessage{activity: &protocol.ToolActivityPayload{Tool: protocol.ToolGrep, Summary: "needle", Status: "success"}, saving: savingNotice(protocol.ToolOutputSummary{Status: status})}
		rendered := strings.Join(renderTranscript([]transcriptMessage{message}, 80), "\n")
		if !strings.Contains(rendered, "Saved output") || strings.Contains(rendered, "Read 0 files") {
			t.Fatal("saving failure hidden in normal group")
		}
	}
}
