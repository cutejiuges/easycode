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
	lists           int
	reads           int
	inheritedOrigin domain.ThreadID
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
	if q.inheritedOrigin.Valid() {
		first.Stdout.Readable, first.Stderr.Readable = false, false
		first.Stdout.SourceThreadID, first.Stderr.SourceThreadID = q.inheritedOrigin, q.inheritedOrigin
	}
	return protocol.TranscriptListPage{Items: []protocol.TranscriptListItem{first, second}}, nil
}

func TestInheritedCommandOutputDoesNotRequestForeignArtifactPages(t *testing.T) {
	m, queries, _ := browserModel(t)
	queries.inheritedOrigin = resumeThreadA
	m, cmd := browserKey(t, m, tea.KeyCtrlO)
	m = updateModel(t, m, cmd())
	m, cmd = browserKey(t, m, tea.KeyEnter)
	m = updateModel(t, m, cmd())
	view := strings.ReplaceAll(m.View(), "\n", "")
	for _, expected := range []string{"/resume " + string(resumeThreadA), "stdout: Saved 100000 bytes · partial prefix", "stderr: Saved 3 bytes · complete", "frozen new"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("inherited saved output lost %q: %s", expected, view)
		}
	}
	m, cmd = browserKey(t, m, tea.KeyTab)
	if cmd != nil || m.browser.stream != "" || queries.reads != 0 {
		t.Fatal("inherited output consumed raw artifact paging")
	}
	m, _ = browserKey(t, m, tea.KeyEsc)
	m, _ = browserKey(t, m, tea.KeyDown)
	m, cmd = browserKey(t, m, tea.KeyEnter)
	updateModel(t, m, cmd())
	if queries.reads != 1 {
		t.Fatal("the current conversation's saved output was no longer prefetched")
	}
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
