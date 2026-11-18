package tui

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

type markdownHydrationQueries struct {
	page  protocol.TranscriptListPage
	text  string
	reads int
}

func (q *markdownHydrationQueries) ListTranscript(context.Context, protocol.TranscriptListQuery) (protocol.TranscriptListPage, error) {
	return q.page, nil
}
func (q *markdownHydrationQueries) ReadTranscriptBody(context.Context, protocol.TranscriptBodyQuery) (protocol.TranscriptBodyPage, error) {
	q.reads++
	return protocol.TranscriptBodyPage{Text: q.text, NextOffset: uint64(len(q.text)), TotalBytes: uint64(len(q.text))}, nil
}
func (q *markdownHydrationQueries) ReadToolOutput(context.Context, protocol.ToolOutputQuery) (protocol.ToolOutputPage, error) {
	return protocol.ToolOutputPage{}, context.Canceled
}
func TestMarkdownHandoffWaitsForBodyAndRetainsNewDelta(t *testing.T) {
	thread := domain.ThreadID("01900000-0000-7000-8000-000000000001")
	turn := domain.TurnID("01900000-0000-7000-8000-000000000002")
	queries := &markdownHydrationQueries{text: "# committed answer\n\nbody", page: protocol.TranscriptListPage{Items: []protocol.TranscriptListItem{{Cursor: protocol.TranscriptCursor{Sequence: 10}, Kind: domain.TranscriptAssistant, TurnID: turn, Text: "summary", More: true}}}}
	model := NewModel(context.Background(), "test", nil).WithTranscriptQueries(thread, queries)
	model.transcript = []transcriptMessage{{role: "Assistant", text: "visible before commit"}}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	next, query := model.commitMainTranscript(10)
	model = next.(Model)
	next, body := model.Update(query())
	model = next.(Model)
	if body == nil || !strings.Contains(model.View(), "visible before commit") || strings.Contains(model.View(), "summary") {
		t.Fatalf("projection replaced before hydration: %s", model.View())
	}
	model.transcript = append(model.transcript, transcriptMessage{role: "Assistant", text: "new delta"})
	model.prepareMarkdownView()
	next, follow := model.Update(body())
	model = next.(Model)
	if follow != nil || queries.reads != 1 || model.main.handoff != nil {
		t.Fatal("hydration did not settle")
	}
	view := model.View()
	if strings.Count(view, "committed answer") != 1 || strings.Count(view, "new delta") != 1 || strings.Contains(view, "visible before commit") {
		t.Fatalf("handoff duplicated or lost content: %s", view)
	}
	reads := queries.reads
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 34, Height: 12})
	if queries.reads != reads {
		t.Fatal("resize performed query")
	}
	if model.main.cancel != nil {
		model.main.cancel()
	}
}
