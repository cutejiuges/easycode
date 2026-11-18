package tui

import (
	"context"
	"reflect"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFileCompletionExclusiveFocusStreamingPasteAndNewline(t *testing.T) {
	setters := map[string]func(*Model){"question": func(m *Model) { m.question = &questionOverlay{} }, "trust": func(m *Model) { m.hookTrust = &hookTrustOverlay{} }, "hooks": func(m *Model) { m.hooks.open = true }, "skills": func(m *Model) { m.skillsOpen = true }, "plugins": func(m *Model) { m.plugins.open = true }, "approval": func(m *Model) { m.approval = &approvalOverlay{} }, "cache": func(m *Model) { m.cachePanel = &protocol.CacheSnapshotPayload{} }, "context": func(m *Model) { m.contextPanel = &protocol.ContextSnapshotPayload{} }, "transcript": func(m *Model) { m.detailOpen = true }, "pending": func(m *Model) { c := m.composer; m.pending = &c }, "retract": func(m *Model) { m.retracting = &inputRetraction{} }, "cancel": func(m *Model) { m.cancelling = true }}
	for name, set := range setters {
		t.Run(name, func(t *testing.T) {
			m, f := fileCompletionModel(t, "prefix @a")
			id := m.files.identity
			before := m.composer
			set(&m)
			m.prepareCompletion()
			if m.files.rows != 0 || len(f.cancels) != 1 || !reflect.DeepEqual(before, m.composer) {
				t.Fatal("exclusive focus failed to cancel names without editing")
			}
			m.question = nil
			m.hookTrust = nil
			m.hooks.open = false
			m.skillsOpen = false
			m.plugins.open = false
			m.approval = nil
			m.cachePanel = nil
			m.contextPanel = nil
			m.detailOpen = false
			m.pending = nil
			m.retracting = nil
			m.cancelling = false
			m = fileReply(t, m, id, 1, []string{"a"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
			if m.files.rows != 0 {
				t.Fatal("modal stale result reopened")
			}
			m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
			if m.files.rows == 0 || m.files.session == id.SessionGeneration || m.composer.text != before.text {
				t.Fatal("explicit reopen failed")
			}
		})
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyCtrlJ}, {Type: tea.KeyEnter, Alt: true}} {
		m, f := fileCompletionModel(t, "@a")
		m = updateModel(t, m, key)
		if m.composer.text != "@a\n" || m.pending != nil || m.files.rows != 0 || len(f.cancels) != 1 {
			t.Fatal("newline accepted file or retained session")
		}
	}
	m, f := fileCompletionModel(t, "")
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("@pasted"), Paste: true})
	if len(f.queries) != 0 || m.files.rows != 0 {
		t.Fatal("paste started discovery")
	}
	m, f = fileCompletionModel(t, "@a")
	before := m.files.identity
	m.activeTurnID = "01900000-0000-7000-8000-000000000010"
	delta, _ := protocol.NewAssistantTextDelta("output")
	delta.TurnID = m.activeTurnID
	m = updateModel(t, m, RuntimeEventMsg{Event: delta})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 28, Height: 10})
	if len(f.queries) != 1 || m.files.identity != before {
		t.Fatal("stream or resize queried again")
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.cancelling || m.files.rows != 0 {
		t.Fatal("first Escape stopped turn")
	}
	m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if !m.cancelling || cmd == nil {
		t.Fatal("second Escape did not stop turn")
	}
}

func TestFileCompletionInvalidSubmitAndLateEffortDoNotTouchNewDraft(t *testing.T) {
	for _, text := range []string{"inspect @../outside", `inspect @"unterminated`, "@a @b @c @d @e @f @g @h @i"} {
		m, _ := fileCompletionModel(t, text)
		before := m.composer
		m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil || m.pending != nil || !reflect.DeepEqual(before, m.composer) || len(m.session.(*fakeChatSession).submits) != 0 || m.errorSummary == "" {
			t.Fatal("invalid draft reached host or changed editor", text)
		}
	}
	s := &effortFixtureSession{}
	f := &fileQueryFixture{results: make(chan domain.FileSearchSnapshot)}
	m := NewModel(context.Background(), "test", s).WithFileQueries(f)
	m.composer.replace(0, 0, "/effort low")
	m, _ = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	id := m.effortFeedback.requestID
	m = effortAck(t, m)
	m.restoreComposer(composerWithText(t, "@a"))
	m.prepareCompletion()
	m = fileReply(t, m, m.files.identity, 1, []string{"a b"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
	before := m.composer
	p := testEffortSnapshot("idle")
	p.Selected = protocol.EffortValuePayload{State: "explicit", Value: "low"}
	p.Source = "runtime"
	p.SelectionRevision = 1
	m = effortEvent(t, m, id, p)
	m.prepareCompletion()
	if !reflect.DeepEqual(before, m.composer) || len(m.effortNotice) != 0 || m.files.rows != 0 || m.effortSelection.selected.Value != "low" {
		t.Fatal("late effort feedback cleared file draft or reopened focus")
	}
}
