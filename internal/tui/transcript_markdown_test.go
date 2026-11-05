package tui

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestMarkdownHandoffRetainsDisplayUntilProjection(t *testing.T) {
	model := snapshotCompletedModel(t)
	thread := domain.ThreadID("01900000-0000-7000-8000-000000000001")
	query := &mainTestQueries{}
	model = model.WithTranscriptQueries(thread, query)
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	before := model.View()
	next, cmd := model.commitMainTranscript(1)
	model = next.(Model)
	model.prepareMarkdownView()
	if cmd == nil || !strings.Contains(model.View(), "● hi") || !strings.Contains(before, "● hi") {
		t.Fatalf("handoff cleared display: %s", model.View())
	}
	model = updateModel(t, model, mainTranscriptReply{generation: model.main.generation, err: context.Canceled})
	if !strings.Contains(model.View(), "● hi") || !strings.Contains(model.View(), "History refresh failed") {
		t.Fatalf("failed handoff lost display: %s", model.View())
	}
	for sequence := uint64(2); sequence < 10; sequence++ {
		next, _ = model.commitMainTranscript(sequence)
		model = next.(Model)
		model.prepareMarkdownView()
		if len(model.main.handoff) > model.transcriptHeight() {
			t.Fatal("unbounded handoff")
		}
	}
	old := model.View()
	model = updateModel(t, model, mainTranscriptReply{generation: model.main.generation - 1})
	if old != model.View() {
		t.Fatal("stale projection changed display")
	}
	if model.main.cancel != nil {
		model.main.cancel()
	}
}
func TestMarkdownViewportAndDetailRawPreserveDraft(t *testing.T) {
	model := NewModel(context.Background(), "test", &fakeChatSession{})
	model.transcript = []transcriptMessage{{role: "Assistant", text: strings.Repeat("line\n\n", 40)}}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 34, Height: 12})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("draft")})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyPgUp})
	if !model.reading.detached {
		t.Fatal("page up did not detach")
	}
	anchor := model.reading.anchor
	model.transcript[0].text += "later\n"
	model.prepareMarkdownView()
	if model.reading.anchor != anchor || model.composer.text != "draft" {
		t.Fatal("new output changed anchor or draft")
	}
	for range 100 {
		model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	if model.reading.detached {
		t.Fatal("bottom did not resume following")
	}
	cursor := protocol.TranscriptCursor{Sequence: 1}
	body := "# title\n\n**bold** text"
	model.detailOpen = true
	model.browser.detail = true
	model.browser.items = []protocol.TranscriptListItem{{Cursor: cursor, Kind: domain.TranscriptAssistant}}
	model.browser.page = &cachedTranscriptPage{body: protocol.TranscriptBodyPage{Text: body, NextOffset: uint64(len(body)), TotalBytes: uint64(len(body))}}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 34, Height: 12})
	if !strings.Contains(model.View(), "bold text") {
		t.Fatalf("missing rich detail: %s", model.View())
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if !strings.Contains(model.View(), "**bold**") || model.composer.text != "draft" {
		t.Fatalf("raw toggle failed: %s", model.View())
	}
}
func TestMarkdownFiftyItemsDoNotReparseOnRepaint(t *testing.T) {
	model := NewModel(context.Background(), "test", nil)
	for i := 0; i < 50; i++ {
		model.main.items = append(model.main.items, transcriptMessage{cursor: protocol.TranscriptCursor{Sequence: uint64(i + 1)}, role: "Assistant", text: "**answer**"})
	}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	count := model.markdown.parser.parses
	for range 100 {
		model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
		model.View()
	}
	if model.markdown.parser.parses != count {
		t.Fatalf("old replies reparsed: %d -> %d", count, model.markdown.parser.parses)
	}
	if len(model.markdown.entries) > 4 {
		t.Fatal("derived document budget exceeded")
	}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 34, Height: 12})
	if model.markdown.parser.parses != count {
		t.Fatal("resize reparsed")
	}
}

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
