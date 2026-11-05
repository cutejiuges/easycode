package tui

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func TestMarkdownAnchorWindowAndRawPaging(t *testing.T) {
	model := NewModel(context.Background(), "test", nil)
	for i := 1; i <= 50; i++ {
		model.main.items = append(model.main.items, transcriptMessage{cursor: protocol.TranscriptCursor{Sequence: uint64(i)}, role: "Assistant", text: "answer"})
	}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 34, Height: 12})
	model.reading.detached = true
	model.reading.anchor = model.reading.anchors[0]
	model.transcript = []transcriptMessage{{role: "Assistant", local: 1, text: "later"}}
	model.prepareMarkdownView()
	if len(model.visibleTranscript()) != 50 || model.reading.anchor.key.cursor.Sequence != 2 || !strings.Contains(model.reading.notice, "left this window") {
		t.Fatalf("missing window fallback: %+v", model.reading.anchor)
	}
	source := strings.Repeat("x", 65530) + "\x1bEND"
	rows, _ := markdownRawWindow(source, 1, 999999, 6, -1)
	var tail strings.Builder
	for _, row := range rows {
		tail.WriteString(row.text)
	}
	if !strings.HasSuffix(tail.String(), "END") || strings.ContainsRune(tail.String(), '\x1b') || len(rows) > 6 {
		t.Fatal("full raw page was not reachable within a six-row window")
	}
	rows, _ = markdownRawWindow("before\n中间\nafter\nmore", 20, 0, 2, len("before\n"))
	if rows[0].text != "中间" || rows[0].source != len("before\n") {
		t.Fatal("source anchor lost on raw toggle")
	}
}

type visibleBodyQueries struct {
	mainTestQueries
	calls []uint64
}

func (q *visibleBodyQueries) ReadTranscriptBody(_ context.Context, query protocol.TranscriptBodyQuery) (protocol.TranscriptBodyPage, error) {
	q.calls = append(q.calls, query.Cursor.Sequence)
	text := "# body " + string(rune('A'+query.Cursor.Sequence-1))
	return protocol.TranscriptBodyPage{Text: text, NextOffset: uint64(len(text)), TotalBytes: uint64(len(text))}, nil
}
func TestMarkdownVisibleBodiesShareFourPages(t *testing.T) {
	q := &visibleBodyQueries{}
	model := NewModel(context.Background(), "test", nil)
	model.browser.queries = q
	model.browser.mainListPinned = true
	model.browser.remember(cachedTranscriptPage{key: model.browser.mainListKey})
	for i := 1; i <= 5; i++ {
		model.main.items = append(model.main.items, transcriptMessage{cursor: protocol.TranscriptCursor{Sequence: uint64(i)}, role: "Assistant", text: "summary", more: true})
		if i == 4 {
			model.main.items = append(model.main.items, transcriptMessage{activity: &protocol.ToolActivityPayload{Tool: protocol.ToolRead, Status: "running", Summary: "README"}})
		}
	}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 45})
	model.prepareMarkdownView()
	for i := 0; i < 12; i++ {
		cmd := model.scheduleHydration()
		if cmd == nil {
			break
		}
		next, _ := model.receiveHydration(cmd().(transcriptHydration))
		model = next.(Model)
		model.prepareMarkdownView()
	}
	if len(q.calls) != 3 || len(model.browser.cache) != 4 || model.scheduleHydration() != nil {
		t.Fatalf("unstable cache: calls=%v pages=%d", q.calls, len(model.browser.cache))
	}
	view := model.View()
	a, call, b := strings.Index(view, "body D"), strings.Index(view, "Read(README)"), strings.Index(view, "body E")
	if a < 0 || call <= a || b <= call || !strings.Contains(view, "budget") {
		t.Fatalf("visible order or folding lost: %s", view)
	}
}

func TestMarkdownDetailSourceAnchorAcrossModeAndWidth(t *testing.T) {
	for _, source := range []string{
		"```go\n" + strings.Repeat("var answer = 42\n", 24) + "```\n",
		"| name | value |\n|---|---|\n" + strings.Repeat("| item | a long value that wraps |\n", 24),
		strings.Repeat("e&#x301; combined\n\n", 24),
	} {
		model := NewModel(context.Background(), "test", nil)
		model.detailOpen = true
		model.browser.detail = true
		model.browser.items = []protocol.TranscriptListItem{{Cursor: protocol.TranscriptCursor{Sequence: 1}, Kind: domain.TranscriptAssistant}}
		model.browser.page = &cachedTranscriptPage{body: protocol.TranscriptBodyPage{Text: source, NextOffset: uint64(len(source)), TotalBytes: uint64(len(source))}}
		model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 12})
		for range 8 {
			model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyDown})
		}
		anchor := model.browser.markdown.sourceAnchor
		if anchor <= 0 {
			t.Fatal("test did not enter document body")
		}
		for _, msg := range []tea.Msg{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}, tea.WindowSizeMsg{Width: 34, Height: 12}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}} {
			model = updateModel(t, model, msg)
			detail := model.browser.markdown
			index := model.detailScroll - detail.first
			if detail.sourceAnchor != anchor || index < 0 || index >= len(detail.rows) || detail.rows[index].source > anchor {
				t.Fatalf("source anchor moved: desired=%d row=%d scroll=%d", anchor, index, model.detailScroll)
			}
		}
	}
}
