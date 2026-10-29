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

func (q *browserQueries) ListCommands(context.Context, protocol.CommandListQuery) (protocol.CommandListPage, error) {
	q.lists++
	return protocol.CommandListPage{Items: []protocol.CommandHistoryItem{
		{Sequence: 2, InvocationID: "new", Command: "printf new", Terminal: true, Status: "success", Preview: "frozen new", Stdout: protocol.CommandOutputSummary{Status: "partial", Readable: true, SavedBytes: 100000, ObservedBytes: 200000}, Stderr: protocol.CommandOutputSummary{Status: "complete", Readable: true, SavedBytes: 3, ObservedBytes: 3, Complete: true}},
		{Sequence: 1, InvocationID: "old", Command: "printf old", Terminal: true, Status: "success", Preview: "frozen old", Stdout: protocol.CommandOutputSummary{Readable: true}},
	}}, nil
}
func (q *browserQueries) ReadCommandOutput(_ context.Context, query protocol.CommandOutputQuery) (protocol.CommandOutputPage, error) {
	q.reads++
	return protocol.CommandOutputPage{Offset: query.Offset, NextOffset: query.Offset + 3, StoredBytes: 100000, Text: string(query.Stream) + " " + query.InvocationID + "\x1b[31m"}, nil
}

func browserModel(t *testing.T) (Model, *browserQueries, *fakeChatSession) {
	t.Helper()
	queries, session := &browserQueries{}, &fakeChatSession{}
	m := NewModel(context.Background(), "test", session).WithCommandQueries(domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef"), queries)
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
	m = updateModel(t, m, cmd())
	if m.browser.selected != 0 || !strings.Contains(m.View(), "Command history") {
		t.Fatal("latest command not selected")
	}
	m, cmd = browserKey(t, m, tea.KeyEnter)
	oldReply := cmd()
	m, cmd = browserKey(t, m, tea.KeyTab)
	if m.browser.stream != protocol.CommandStderr {
		t.Fatal("stream not switched")
	}
	m = updateModel(t, m, oldReply)
	if m.browser.page != nil {
		t.Fatal("late stdout reply replaced stderr")
	}
	m = updateModel(t, m, cmd())
	if m.browser.page == nil || !strings.Contains(m.browser.page.Text, "stderr new") {
		t.Fatal("stderr page missing")
	}
	if strings.Contains(m.View(), "\x1b[31m") || !strings.Contains(m.View(), "frozen new") {
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
	m = updateModel(t, m, cmd())
	m, cmd = browserKey(t, m, tea.KeyEnter)
	m = updateModel(t, m, cmd())
	for i := 0; i < 6; i++ {
		m, cmd = browserKey(t, m, tea.KeyPgDown)
		m = updateModel(t, m, cmd())
	}
	if len(m.browser.cache) != 4 || m.browser.page.Offset != 18 {
		t.Fatal("page cache is not bounded")
	}
	m, cmd = browserKey(t, m, tea.KeyTab)
	m = updateModel(t, m, cmd())
	if m.browser.page.Offset != 0 {
		t.Fatal("stream reused stdout cursor")
	}
	reads := q.reads
	m, cmd = browserKey(t, m, tea.KeyTab)
	if cmd != nil || q.reads != reads || m.browser.page.Offset != 18 {
		t.Fatal("cached stream cursor lost")
	}
	m, cmd = browserKey(t, m, tea.KeyPgUp)
	if cmd != nil || m.browser.page.Offset != 15 {
		t.Fatal("previous raw boundary lost")
	}
}

func TestCommandBrowserSessionChangeInvalidatesOldReplies(t *testing.T) {
	m, queries, _ := browserModel(t)
	m, cmd := browserKey(t, m, tea.KeyCtrlO)
	old := cmd()
	m = m.WithCommandQueries(domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdee"), queries)
	m, _ = browserKey(t, m, tea.KeyCtrlO)
	m = updateModel(t, m, old)
	if len(m.browser.items) != 0 {
		t.Fatal("old session history entered new session")
	}
}
