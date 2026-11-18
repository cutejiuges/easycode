package tui

import (
	"context"
	"reflect"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type effortFixtureSession struct {
	contextFixtureSession
	choices  []domain.EffortChoice
	requests []protocol.RequestID
}

func (s *effortFixtureSession) QueryEffort(_ context.Context, id protocol.RequestID) (protocol.CommandResult, error) {
	s.requests = append(s.requests, id)
	cmd, _ := protocol.NewQueryEffortCommand(id)
	return protocol.NewAcceptedCommandResult(cmd, protocol.CommandDispositionQueried)
}
func (s *effortFixtureSession) SetEffort(_ context.Context, id protocol.RequestID, c domain.EffortChoice) (protocol.CommandResult, error) {
	s.requests = append(s.requests, id)
	s.choices = append(s.choices, c)
	cmd, _ := protocol.NewSetEffortCommand(id, c)
	return protocol.NewAcceptedCommandResult(cmd, protocol.CommandDispositionUpdated)
}

func effortAck(t *testing.T, m Model) Model {
	t.Helper()
	f := m.effortFeedback
	var cmd protocol.Command
	if f.kind == protocol.CommandQueryEffort {
		cmd, _ = protocol.NewQueryEffortCommand(f.requestID)
	} else {
		cmd, _ = protocol.NewSetEffortCommand(f.requestID, domain.NewAutomaticEffortChoice())
	}
	disposition := protocol.CommandDispositionUpdated
	if f.kind == protocol.CommandQueryEffort {
		disposition = protocol.CommandDispositionQueried
	}
	result, _ := protocol.NewAcceptedCommandResult(cmd, disposition)
	item, _ := protocol.NewCommandResultItem(result)
	next, _ := m.projectControlItem(item)
	return next.(Model)
}
func effortEvent(t *testing.T, m Model, id protocol.RequestID, p protocol.EffortSnapshotPayload) Model {
	t.Helper()
	event, err := protocol.NewEffortSnapshot("01900000-0000-7000-8000-000000000001", "01900000-0000-7000-8000-000000000002", p)
	if err != nil {
		t.Fatal(err)
	}
	item, err := protocol.NewCorrelatedContextItem(id, event)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.projectControlItem(item)
	return next.(Model)
}

func TestEffortSameRevisionOldOperationAndNewOverlayCannotRestoreFeedback(t *testing.T) {
	m := NewModel(context.Background(), "test", &effortFixtureSession{})
	m.composer.replace(0, 0, "/effort status")
	m, _ = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	id := m.effortFeedback.requestID
	m = effortAck(t, m)
	old := testEffortSnapshot("idle")
	started := protocol.NewTurnStarted()
	started.SessionID = "01900000-0000-7000-8000-000000000001"
	started.ThreadID = "01900000-0000-7000-8000-000000000002"
	started.TurnID = "01900000-0000-7000-8000-000000000003"
	m = updateModel(t, m, RuntimeEventMsg{Event: started})
	draft := m.composer
	question := &questionOverlay{}
	m.question = question
	m = effortEvent(t, m, id, old)
	if m.question != question || len(m.effortNotice) != 0 || !reflect.DeepEqual(draft, m.composer) {
		t.Fatal("same revision stale operation restored feedback")
	}
}

func TestEffortDuplicateAndConflictingRevisionCannotChangeDraft(t *testing.T) {
	m := NewModel(context.Background(), "test", &effortFixtureSession{})
	m.composer.replace(0, 0, "/effort status")
	m, _ = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	id := m.effortFeedback.requestID
	m = effortAck(t, m)
	m = effortAck(t, m)
	p := testEffortSnapshot("idle")
	m = effortEvent(t, m, id, p)
	if len(m.composer.history.entries) != 1 || len(m.effortNotice) != 2 {
		t.Fatal("duplicate receipt consumed feedback twice")
	}
	m.composer.replace(0, 0, "new draft")
	draft := m.composer
	m = effortEvent(t, m, id, p)
	if !reflect.DeepEqual(draft, m.composer) || len(m.composer.history.entries) != 1 {
		t.Fatal("duplicate snapshot cleared a later draft")
	}
	selected := *m.effortSelection
	p.Selected = protocol.EffortValuePayload{State: "explicit", Value: "low"}
	p.Source = "runtime"
	if m.observeEffort(p, true) || *m.effortSelection != selected {
		t.Fatal("contradictory same-revision selection replaced authority")
	}
	p.SelectionRevision++
	if m.observeEffort(p, false) || m.effortSelection.selected.Value != "low" || !reflect.DeepEqual(draft, m.composer) {
		t.Fatal("newer non-local selection restored feedback or lost authority")
	}
}
