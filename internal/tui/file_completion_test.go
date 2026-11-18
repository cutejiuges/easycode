package tui

import (
	"context"
	"testing"

	"easycode/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
)

type fileQueryFixture struct {
	queries []domain.FileSearchQuery
	cancels []domain.FileSearchIdentity
	results chan domain.FileSearchSnapshot
}

func (f *fileQueryFixture) QueryFiles(q domain.FileSearchQuery) error {
	f.queries = append(f.queries, q)
	return nil
}
func (f *fileQueryFixture) CancelFiles(id domain.FileSearchIdentity) {
	f.cancels = append(f.cancels, id)
}
func (f *fileQueryFixture) FileResults() <-chan domain.FileSearchSnapshot { return f.results }
func fileCompletionModel(t *testing.T, text string) (Model, *fileQueryFixture) {
	t.Helper()
	f := &fileQueryFixture{results: make(chan domain.FileSearchSnapshot, 8)}
	m := NewModel(context.Background(), "test", &fakeChatSession{}).WithFileQueries(f)
	if err := m.composer.replace(0, 0, text); err != nil {
		t.Fatal(err)
	}
	m.prepareCompletion()
	return m, f
}
func fileReply(t *testing.T, m Model, id domain.FileSearchIdentity, revision uint64, paths []string, state domain.FileSearchState, report domain.FileDiscoveryReport) Model {
	t.Helper()
	snapshot, err := domain.NewFileSearchSnapshot(id, revision, paths, state, report)
	if err != nil {
		t.Fatal(err)
	}
	return updateModel(t, m, fileResultMsg{generation: m.files.workspace, snapshot: snapshot})
}

func TestFileCompletionRejectsOldRowsABAAndOldIndexRevision(t *testing.T) {
	m, f := fileCompletionModel(t, "@a")
	a := m.files.identity
	waiter := m.files.waiter
	m = fileReply(t, m, a, 2, []string{"a-first", "a-next"}, domain.FileSearchProgress, domain.FileDiscoveryReport{})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	b := m.files.identity
	if m.files.ready || len(m.files.paths) != 0 || a.SessionGeneration != b.SessionGeneration || a.RequestGeneration == b.RequestGeneration {
		t.Fatal("pending query left stale rows or restarted session")
	}
	m = fileReply(t, m, a, 3, []string{"a-wrong"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
	m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil || m.composer.text != "@ab" || m.files.ready {
		t.Fatal("stale Tab wrote wrong path")
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	newest := m.files.identity
	m = fileReply(t, m, a, 4, []string{"a-wrong"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
	if m.files.ready {
		t.Fatal("ABA revived old request")
	}
	m = fileReply(t, m, newest, 3, []string{"a-first", "a-next"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
	if m.files.selected != 1 {
		t.Fatal("path selection was not preserved")
	}
	m = fileReply(t, m, newest, 2, []string{"a-old-index"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
	if m.files.paths[1] != "a-next" || len(f.queries) != 3 || waiter == nil {
		t.Fatal("old index replaced newer snapshot")
	}
	beforeWaiter := m.files.waiter
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.files.waiter != beforeWaiter {
		t.Fatal("key started another result waiter")
	}
}

func TestFileCompletionRebindWaiterAndPendingActiveCancellation(t *testing.T) {
	m, f := fileCompletionModel(t, "@a")
	old := m.files.waiter
	oldWait := m.initialFileWait()
	fresh := &fileQueryFixture{results: make(chan domain.FileSearchSnapshot, 1)}
	m = m.WithFileQueries(fresh)
	if m.files.waiter != old || !old.stopped || len(f.cancels) != 1 {
		t.Fatal("rebind abandoned old waiter")
	}
	msg := oldWait().(fileResultMsg)
	m, cmd := updateModelWithCommand(t, m, msg)
	if cmd == nil || m.files.waiter == old || m.files.waiter.generation != m.files.workspace {
		t.Fatal("new waiter did not follow old completion")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.pendingCancel = cancel
	c := m.composer
	m.pending = &c
	m.activeTurnID = "01900000-0000-7000-8000-000000000001"
	m, cmd = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if ctx.Err() == nil || cmd == nil || !m.cancelling {
		t.Fatal("active stop did not cancel pending preparation")
	}
	m.closeFileQueries()
	if !m.files.waiter.stopped {
		t.Fatal("quit retained waiter")
	}
}
