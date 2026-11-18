package tui

import (
	"context"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

type contextFixtureSession struct {
	fakeChatSession
	instructions string
	cancelled    domain.CompactionID
}

func (session *contextFixtureSession) QueryContext(_ context.Context, requestID protocol.RequestID) (protocol.CommandResult, error) {
	command, _ := protocol.NewQueryContextCommand(requestID)
	result, err := protocol.NewAcceptedCommandResult(command, protocol.CommandDispositionQueried)
	item, _ := protocol.NewCommandResultItem(result)
	session.outputs <- item
	return result, err
}
func (session *contextFixtureSession) CompactContext(_ context.Context, instructions string) (protocol.CommandResult, error) {
	session.instructions = instructions
	command, _ := protocol.NewCompactContextCommand("compact", instructions)
	result, err := protocol.NewAcceptedCompactionResult(command, "01900000-0000-7000-8000-000000000009")
	item, _ := protocol.NewCommandResultItem(result)
	session.outputs <- item
	return result, err
}
func (session *contextFixtureSession) CancelCompaction(_ context.Context, id domain.CompactionID) (protocol.CommandResult, error) {
	session.cancelled = id
	command, _ := protocol.NewCancelCompactionCommand("cancel", id)
	return protocol.NewAcceptedCommandResult(command, protocol.CommandDispositionCancelling)
}

func TestContextRejectionAndStaleCompletionPreserveDraft(t *testing.T) {
	model := NewModel(context.Background(), "test", &contextFixtureSession{})
	if err := model.composer.replace(0, 0, "/compact keep failures"); err != nil {
		t.Fatal(err)
	}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = next.(Model)
	draft := model.composer
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	command, _ := protocol.NewCompactContextCommand("compact", "keep failures")
	result, _ := protocol.NewRejectedCommandResult(command, protocol.ControlErrorSessionBusy)
	next, _ = model.projectContextResult(result)
	model = next.(Model)
	if !reflect.DeepEqual(model.composer, draft) || model.activeCompactionID != "" {
		t.Fatal("rejection lost draft or cursor")
	}
	model.activeCompactionID = "01900000-0000-7000-8000-000000000009"
	event, _ := protocol.NewCompactionFailed("01900000-0000-7000-8000-000000000001", "01900000-0000-7000-8000-000000000002", "01900000-0000-7000-8000-000000000008", protocol.CompactionFailedPayload{Scope: domain.ManualCompactionScope(), Code: protocol.CompactionFailureCancelled, Durable: true, UsageState: "unavailable"})
	next, _ = model.projectContextEvent(event)
	model = next.(Model)
	if !model.activeCompactionID.Valid() || !reflect.DeepEqual(model.composer, draft) {
		t.Fatal("stale terminal changed current operation")
	}
	model.contextPanel = &protocol.ContextSnapshotPayload{}
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.contextPanel != nil || cmd != nil || !model.activeCompactionID.Valid() {
		t.Fatal("panel escape cancelled operation")
	}
}

func testEffortSnapshot(activity string) protocol.EffortSnapshotPayload {
	value := protocol.EffortValuePayload{State: "omitted"}
	result := protocol.EffortSnapshotPayload{Scope: "process", Family: domain.ProviderOpenAI, Startup: value, Selected: value, Source: "startup", Activity: activity, AllowedLevels: []domain.EffortLevel{"none", "minimal", "low", "medium", "high", "xhigh", "max"}}
	if activity != "idle" {
		active, _ := protocol.NewTurnEffortActive("01900000-0000-7000-8000-000000000001", "running", value, 0, false)
		result.Active = &active
	}
	return result
}
