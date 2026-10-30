package tui

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type browserQueries struct {
	lists int
	reads int
}

func (q *browserQueries) ListTranscript(context.Context, protocol.TranscriptListQuery) (protocol.TranscriptListPage, error) {
	q.lists++
	turn := domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcdea")
	activity := protocol.ToolActivityPayload{Tool: protocol.ToolShell, InvocationID: "018f1d8a-7b5c-7def-8123-456789abcdeb", Summary: "printf new", Status: "success", Code: "ok", ExitKnown: true}
	first := protocol.TranscriptListItem{Cursor: protocol.TranscriptCursor{Sequence: 2}, Kind: domain.TranscriptCall, TurnID: turn, Activity: activity, Stdout: protocol.ToolOutputSummary{Status: "partial", Reason: "stream_limit", Readable: true, SavedBytes: 100000, ObservedBytes: 200000}, Stderr: protocol.ToolOutputSummary{Status: "complete", Readable: true, SavedBytes: 3, ObservedBytes: 3, Complete: true}}
	second := first
	second.Cursor.Sequence = 1
	second.Activity.InvocationID = "018f1d8a-7b5c-7def-8123-456789abcdec"
	second.Activity.Summary = "printf old"
	return protocol.TranscriptListPage{Items: []protocol.TranscriptListItem{first, second}}, nil
}
func (q *browserQueries) ReadTranscriptBody(_ context.Context, query protocol.TranscriptBodyQuery) (protocol.TranscriptBodyPage, error) {
	text := "frozen new"
	if query.Cursor.Sequence == 1 {
		text = "frozen old"
	}
	return protocol.TranscriptBodyPage{Text: text, NextOffset: uint64(len(text)), TotalBytes: uint64(len(text))}, nil
}
func (q *browserQueries) ReadToolOutput(_ context.Context, query protocol.ToolOutputQuery) (protocol.ToolOutputPage, error) {
	q.reads++
	text := string(query.Stream) + " new\x1b[31m"
	offset := query.Offset
	if query.Backward {
		offset -= uint64(len(text))
	}
	return protocol.ToolOutputPage{Offset: offset, NextOffset: offset + uint64(len(text)), StoredBytes: 100000, Text: text}, nil
}

func browserModel(t *testing.T) (Model, *browserQueries, *fakeChatSession) {
	t.Helper()
	queries, session := &browserQueries{}, &fakeChatSession{}
	m := NewModel(context.Background(), "test", session).WithTranscriptQueries(domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef"), queries)
	m.width, m.height = 80, 24
	m.composer = composerWithText(t, "kept draft")
	return m, queries, session
}

func browserKey(t *testing.T, model Model, key tea.KeyType) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := model.Update(tea.KeyMsg{Type: key})
	return next.(Model), cmd
}

func TestCommandBrowserNavigationAndStaleReplies(t *testing.T) {
	m, queries, session := browserModel(t)
	m, cmd := browserKey(t, m, tea.KeyCtrlO)
	if queries.lists != 0 || !m.detailOpen {
		t.Fatal("opening performed synchronous I/O")
	}
	if cmd != nil {
		m = updateModel(t, m, cmd())
	}
	if m.browser.selected != 0 || !strings.Contains(m.View(), "Transcript") {
		t.Fatal("latest command not selected")
	}
	m, cmd = browserKey(t, m, tea.KeyEnter)
	oldReply := cmd()
	m, cmd = browserKey(t, m, tea.KeyTab)
	if m.browser.stream != protocol.ToolStderr {
		t.Fatal("stream not switched")
	}
	m = updateModel(t, m, oldReply)
	if m.browser.page != nil {
		t.Fatal("late stdout reply replaced stderr")
	}
	if cmd != nil {
		m = updateModel(t, m, cmd())
	}
	if m.browser.page == nil || !strings.Contains(m.browser.page.output.Text, "stderr new") {
		t.Fatal("stderr page missing")
	}
	if strings.Contains(m.View(), "\x1b[31m") {
		t.Fatal("unsafe display or frozen preview lost")
	}
	reads := queries.reads
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 60, Height: 20})
	_ = m.View()
	_ = m.View()
	if queries.reads != reads {
		t.Fatal("render performed I/O")
	}
	m, _ = browserKey(t, m, tea.KeyEsc)
	if !m.detailOpen || m.browser.detail {
		t.Fatal("Esc did not return to list")
	}
	m, _ = browserKey(t, m, tea.KeyDown)
	m, cmd = browserKey(t, m, tea.KeyEnter)
	late := cmd()
	m, _ = browserKey(t, m, tea.KeyCtrlO)
	m = updateModel(t, m, late)
	if m.detailOpen || m.browser.page != nil || len(m.browser.cache) != 0 {
		t.Fatal("closed overlay accepted late result")
	}
	if m.composer.text != "kept draft" || len(session.submits) != 0 {
		t.Fatal("browser keys leaked into composer")
	}
}

func TestCommandBrowserCacheAndIndependentOffsets(t *testing.T) {
	m, q, _ := browserModel(t)
	m, cmd := browserKey(t, m, tea.KeyCtrlO)
	if cmd != nil {
		m = updateModel(t, m, cmd())
	}
	m, cmd = browserKey(t, m, tea.KeyEnter)
	if cmd != nil {
		m = updateModel(t, m, cmd())
	}
	m, cmd = browserKey(t, m, tea.KeyRight)
	if cmd != nil {
		m = updateModel(t, m, cmd())
	}
	for i := 0; i < 6; i++ {
		m, cmd = browserKey(t, m, tea.KeyPgDown)
		if cmd != nil {
			m = updateModel(t, m, cmd())
		}
	}
	if len(m.browser.cache) != 4 || m.browser.page.output.Offset != 90 {
		t.Fatal("page cache is not bounded")
	}
	m, cmd = browserKey(t, m, tea.KeyTab)
	if cmd != nil {
		m = updateModel(t, m, cmd())
	}
	if m.browser.page.output.Offset != 0 {
		t.Fatal("stream reused stdout cursor")
	}
	reads := q.reads
	m, cmd = browserKey(t, m, tea.KeyTab)
	if cmd != nil || q.reads != reads || m.browser.page.output.Offset != 90 {
		t.Fatal("cached stream cursor lost")
	}
	m, cmd = browserKey(t, m, tea.KeyPgUp)
	if cmd != nil || m.browser.page.output.Offset != 75 {
		t.Fatal("previous raw boundary lost")
	}
}

func TestCommandBrowserSessionChangeInvalidatesOldReplies(t *testing.T) {
	m, queries, _ := browserModel(t)
	m, cmd := browserKey(t, m, tea.KeyCtrlO)
	old := cmd()
	m = m.WithTranscriptQueries(domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdee"), queries)
	m, _ = browserKey(t, m, tea.KeyCtrlO)
	m = updateModel(t, m, old)
	if len(m.browser.items) != 0 {
		t.Fatal("old session history entered new session")
	}
}

func TestBrowserReversePaginationBeyondFourCachedPages(t *testing.T) {
	m, _, _ := browserModel(t)
	for _, key := range []tea.KeyType{tea.KeyCtrlO, tea.KeyEnter, tea.KeyRight} {
		next, cmd := browserKey(t, m, key)
		m = next
		if cmd != nil {
			m = updateModel(t, m, cmd())
		}
	}
	for i := 0; i < 8; i++ {
		next, cmd := browserKey(t, m, tea.KeyPgDown)
		m = next
		if cmd != nil {
			m = updateModel(t, m, cmd())
		}
	}
	for i := 0; i < 8; i++ {
		end := m.browser.page.output.Offset
		next, cmd := browserKey(t, m, tea.KeyPgUp)
		m = next
		if cmd != nil {
			m = updateModel(t, m, cmd())
		}
		if m.browser.page.output.NextOffset != end || len(m.browser.cache) > 4 {
			t.Fatal("reverse page skipped bytes or cache grew")
		}
	}
	if m.browser.page.output.Offset != 0 {
		t.Fatal("reverse pagination did not reach start")
	}
}
