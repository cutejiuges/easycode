package tui

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestQueuedRetractionPreservesDraftAndFreezesEdits(t *testing.T) {
	host := &fakeChatSession{}
	model := NewModel(context.Background(), "test", host)
	id := domain.InputID("01900000-0000-7000-8000-000000000001")
	model.inputs = []inputPreview{{id: id, text: " queued 中文\n"}}
	model.composer = composerWithText(t, "draft")
	next, command := model.retractQueuedInputs()
	model = next.(Model)
	if command == nil || model.retracting == nil || model.composer.text != "draft" {
		t.Fatal("retraction changed draft before acceptance")
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ignored")})
	if model.composer.text != "draft" {
		t.Fatal("pending retraction allowed editing")
	}
	command()
	model = updateModel(t, model, controlItemMsg{item: <-host.outputs})
	model = updateModel(t, model, controlItemMsg{item: <-host.outputs})
	if model.composer.text != " queued 中文\n\ndraft" || len(model.inputs) != 0 || model.retracting != nil || len(host.submits) != 0 {
		t.Fatal("retraction lost input or executed it")
	}
}

func TestReturnedTrayPreservesLargeInputsAndExistingDraft(t *testing.T) {
	host := &fakeChatSession{}
	model := NewModel(context.Background(), "test", host)
	large := strings.Repeat("x", domain.MaxUserInputBytes)
	model.inputs = []inputPreview{{text: large, state: protocol.InputReturned}, {text: "second", state: protocol.InputReturned}, {text: "uncertain", state: protocol.InputUncertain}}
	model.composer = composerWithText(t, "existing")
	next, cmd := model.handleReturnedInput(true)
	model = next.(Model)
	if cmd != nil || len(model.inputs) != 3 || model.composer.text != "existing" {
		t.Fatal("oversized take lost text")
	}
	next, cmd = model.handleReturnedInput(false)
	model = next.(Model)
	if cmd != nil || len(model.inputs) != 2 || model.composer.text != "existing" {
		t.Fatal("discard changed existing draft")
	}
	next, cmd = model.handleReturnedInput(true)
	model = next.(Model)
	if cmd != nil || len(model.inputs) != 1 || model.composer.text != "second\nexisting" || len(host.submits) != 0 {
		t.Fatal("take executed or overwrote draft")
	}
	next, cmd = model.handleReturnedInput(true)
	model = next.(Model)
	if cmd != nil || len(model.inputs) != 1 {
		t.Fatal("uncertain input can be retried")
	}
}

func TestQueuedRetractionSizePreflightHasNoCommand(t *testing.T) {
	model := NewModel(context.Background(), "test", &fakeChatSession{})
	model.inputs = []inputPreview{{text: strings.Repeat("x", domain.MaxUserInputBytes)}, {text: "next"}}
	next, cmd := model.retractQueuedInputs()
	model = next.(Model)
	if cmd != nil || model.retracting != nil || len(model.inputs) != 2 || model.composer.text != "" || !strings.Contains(model.errorSummary, "input_too_large") {
		t.Fatal("oversized retraction mutated queue")
	}
}

func TestInputStatusPreviewFixedWidthSnapshot(t *testing.T) {
	model := NewModel(context.Background(), "test", &fakeChatSession{})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.inputs = []inputPreview{{text: "queued\ntext"}, {text: "claimed", state: protocol.InputClaimed}, {text: "returned", state: protocol.InputReturned}, {text: "uncertain", state: protocol.InputUncertain}}
	const want = "[queued] queued ↵ text\n[claimed] claimed\n[returned] returned\n[uncertain] uncertain\nUp/Esc: edit queued · Ctrl+R: take returned · Ctrl+D: discard returned\n"
	if got := model.renderInputs(); got != want {
		t.Fatalf("preview snapshot: %q", got)
	}
}

func TestPendingFollowupEscapeStopsCurrentTurn(t *testing.T) {
	model := NewModel(context.Background(), "test", &fakeChatSession{})
	model.activeTurnID = "01900000-0000-7000-8000-000000000010"
	draft := composerWithText(t, "follow-up")
	model.composer = draft
	model.pending = &draft
	model.state = stateSubmitting
	next, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if cmd == nil || !model.cancelling || model.interruptNext || model.composer.text != "follow-up" {
		t.Fatal("pending follow-up Escape did not target active turn")
	}
}

func TestRejectedAdmissionClearsDeferredStop(t *testing.T) {
	model := NewModel(context.Background(), "test", &fakeChatSession{})
	draft := composerWithText(t, "retry")
	model.composer = draft
	model.pending = &draft
	model.state = stateSubmitting
	model.interruptNext = true
	command, _ := protocol.NewSubmitConversationalInputCommand("request-1", "retry")
	result, err := protocol.NewRejectedCommandResult(command, protocol.ControlErrorInputQueueFull)
	if err != nil {
		t.Fatal(err)
	}
	item, err := protocol.NewCommandResultItem(result)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := model.projectControlItem(item)
	model = next.(Model)
	if model.interruptNext || model.pending != nil || model.composer.text != "retry" || len(model.inputs) != 0 {
		t.Fatal("rejection retained cancellation intent")
	}
}
