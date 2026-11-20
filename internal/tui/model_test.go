package tui

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

type fakeChatSession struct {
	mu                  sync.Mutex
	submits             []string
	events              chan protocol.Event
	outputs             chan protocol.ControlItem
	submitErr           error
	interrupts          int
	lastInterruptedTurn domain.TurnID
	contexts            []context.Context
	approvals           []capturedApproval
}

type capturedApproval struct {
	turnID   domain.TurnID
	request  protocol.ToolApprovalRequestedPayload
	decision protocol.ApprovalDecision
	feedback string
}

func (session *fakeChatSession) Outputs() <-chan protocol.ControlItem {
	if session.outputs == nil {
		session.outputs = make(chan protocol.ControlItem, 64)
	}
	return session.outputs
}
func (session *fakeChatSession) Append(ctx context.Context, draft domain.InputDraft) (protocol.CommandResult, error) {
	text := draft.Text()
	session.mu.Lock()
	session.contexts = append(session.contexts, ctx)
	session.submits = append(session.submits, text)
	err := session.submitErr
	session.mu.Unlock()
	if err != nil {
		return protocol.CommandResult{}, err
	}
	command, _ := protocol.NewSubmitConversationalInputCommand("ui-input", text)
	result, _ := protocol.NewAcceptedInputResult(command, protocol.CommandDispositionStarting, "01900000-0000-7000-8000-000000000001")
	item, _ := protocol.NewCommandResultItem(result)
	session.outputs <- item
	if session.events != nil {
		session.sendEvent(protocol.NewTurnStarted())
		input, _ := domain.NewUserInput(result.EntryID(), text)
		event, _ := protocol.NewUserInputCommitted(input)
		session.sendEvent(event)
		for len(session.events) > 0 {
			session.sendEvent(<-session.events)
		}
	}
	return result, nil
}
func (session *fakeChatSession) Retract(ctx context.Context, ids []domain.InputID) (protocol.CommandResult, error) {
	command, err := protocol.NewRetractInputsCommand("ui-retract", ids)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	result, err := protocol.NewAcceptedCommandResult(command, protocol.CommandDispositionRetracted)
	if err != nil {
		return result, err
	}
	item, _ := protocol.NewCommandResultItem(result)
	session.outputs <- item
	for _, id := range ids {
		status, _ := protocol.NewInputStatus("ui-input", id, protocol.InputReturned, "", protocol.InputReasonRetracted)
		item, _ := protocol.NewInputStatusItem(status)
		session.outputs <- item
	}
	return result, nil
}
func (session *fakeChatSession) sendEvent(event protocol.Event) {
	event.SessionID = "01900000-0000-7000-8000-000000000011"
	event.ThreadID = "01900000-0000-7000-8000-000000000012"
	event.TurnID = "018f1d8a-7b5c-7def-8234-456789abcdef"
	item, err := protocol.NewCorrelatedTurnItem("ui-input", event)
	if err != nil {
		panic(err)
	}
	session.outputs <- item
}

func testAdmissionMessage(model Model, command tea.Cmd) tea.Msg {
	message := command()
	if message != nil {
		return message
	}
	return waitForControl(model.outputs)()
}

func (session *fakeChatSession) InterruptTurn(turnID domain.TurnID) {
	session.mu.Lock()
	session.interrupts++
	session.lastInterruptedTurn = turnID
	session.mu.Unlock()
}

func (session *fakeChatSession) ResolveApproval(_ context.Context, turnID domain.TurnID, request protocol.ToolApprovalRequestedPayload, decision protocol.ApprovalDecision, feedback string) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.approvals = append(session.approvals, capturedApproval{turnID: turnID, request: request, decision: decision, feedback: feedback})
	return nil
}

func TestApprovalFeedbackEscapeAndResolutionLifecycle(t *testing.T) {
	session := &fakeChatSession{}
	model := NewModel(context.Background(), "test", session)
	event := mustApprovalRequested(t)
	model = updateModel(t, model, RuntimeEventMsg{Event: event})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("请保留旧名")})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.approval == nil || model.approval.editing || session.interrupts != 0 {
		t.Fatal("首次 Esc 未只退出反馈编辑")
	}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	_ = command()
	if len(session.approvals) != 1 || session.approvals[0].decision != protocol.ApprovalDeny || session.approvals[0].feedback != "请保留旧名" {
		t.Fatalf("deny approval = %#v", session.approvals)
	}
	resolved, err := protocol.NewToolApprovalResolved(protocol.ToolApprovalResolvedPayload{RequestID: eventApprovalRequestID, InvocationID: eventApprovalInvocationID, Fingerprint: strings.Repeat("a", 64), Decision: protocol.ApprovalDeny})
	if err != nil {
		t.Fatal(err)
	}
	resolved.TurnID = event.TurnID
	model = updateModel(t, model, RuntimeEventMsg{Event: resolved})
	if model.approval != nil {
		t.Fatal("resolved event did not close overlay")
	}
}

func TestApprovalOwnsKeysOverSkillsUntilTurnCleanup(t *testing.T) {
	session := &fakeChatSession{}
	model := NewModel(context.Background(), "test", session).WithSkills([]domain.SkillListEntry{
		completionSkillEntry(t, "first", "First skill"),
		completionSkillEntry(t, "second", "Second skill"),
	})
	model.skillsOpen = true
	model.skillSelection = 1
	model.composer = composerWithText(t, "keep draft")
	event := mustApprovalRequested(t)
	model.activeTurnID = event.TurnID
	model = updateModel(t, model, RuntimeEventMsg{Event: event})
	if !strings.Contains(model.View(), "--- a/src/main.go") {
		t.Fatal("Skills hid the pending approval")
	}
	model, stop := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyCtrlC})
	if stop == nil || !model.approval.submitted {
		t.Fatal("approval did not enter cancellation")
	}
	stop()
	for _, key := range []tea.KeyType{tea.KeyCtrlC, tea.KeyEsc, tea.KeyEnter} {
		model, stop = updateModelWithCommand(t, model, tea.KeyMsg{Type: key})
		if stop != nil {
			t.Fatal("approval accepted another action during cancellation", key)
		}
	}
	if session.interrupts != 1 || session.lastInterruptedTurn != event.TurnID || len(session.approvals) != 0 {
		t.Fatal("approval did not cancel the displayed turn exactly once")
	}
	completed := mustRuntimeCompleted(t)
	completed.TurnID = event.TurnID
	model = updateModel(t, model, RuntimeEventMsg{Event: completed})
	if model.approval != nil || !strings.Contains(model.View(), "> /second") || model.composer.text != "keep draft" {
		t.Fatal("turn cleanup lost the Skills position or draft")
	}
}

const (
	eventApprovalRequestID    = "018f1d8a-7b5c-7def-8123-456789abcdef"
	eventApprovalInvocationID = "018f1d8a-7b5c-7def-8abc-456789abcdef"
)

func mustApprovalRequested(t *testing.T) protocol.Event {
	t.Helper()
	event, err := protocol.NewToolApprovalRequested(protocol.ToolApprovalRequestedPayload{
		RequestID: eventApprovalRequestID, InvocationID: eventApprovalInvocationID,
		ProviderCallID: "call-patch", Capability: "fs.patch", Reason: "Patch modifies an existing workspace file.",
		RelativePath: "src/main.go", Fingerprint: strings.Repeat("a", 64),
		Presentation: &protocol.ApprovalPresentation{Kind: protocol.PresentationPatch, Patch: &protocol.PatchApprovalPresentation{Diff: "--- a/src/main.go\n+++ b/src/main.go\n-old\n+new\n"}},
		Decisions:    []protocol.ApprovalDecision{protocol.ApprovalAllowOnce, protocol.ApprovalAllowSession, protocol.ApprovalDeny},
	})
	if err != nil {
		t.Fatal(err)
	}
	event.TurnID, err = domain.ParseTurnID("018f1d8a-7b5c-7def-8234-456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestModelSubmitsAndProjectsOrderedDeltas(t *testing.T) {
	session := &fakeChatSession{}
	type contextKey struct{}
	submitContext := context.WithValue(context.Background(), contextKey{}, "operation")
	model := NewModel(submitContext, "test", session)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if command == nil || model.state != stateSubmitting || len(model.transcript) != 0 {
		t.Fatalf("submit state: %#v", model)
	}
	ready := testAdmissionMessage(model, command)
	model, command = updateModelWithCommand(t, model, ready)
	if command == nil {
		t.Fatal("missing event wait command")
	}

	model = testCommitAcceptedInput(t, model)
	first, _ := protocol.NewAssistantTextDelta("hel")
	second, _ := protocol.NewAssistantTextDelta("lo")
	model = updateModel(t, model, RuntimeEventMsg{Event: first})
	model = updateModel(t, model, RuntimeEventMsg{Event: second})
	model = updateModel(t, model, RuntimeEventMsg{Event: mustRuntimeCompleted(t)})
	if model.state != stateIdle || model.outputs == nil {
		t.Fatal("terminal closed the long-lived control consumer")
	}

	if len(model.transcript) != 2 || model.transcript[1].text != "hello" {
		t.Fatalf("transcript: %#v", model.transcript)
	}
	if len(session.submits) != 1 || session.submits[0] != "hello" {
		t.Fatalf("submits: %#v", session.submits)
	}
	if len(session.contexts) != 1 || session.contexts[0].Value(contextKey{}) != "operation" || session.contexts[0].Err() != context.Canceled {
		t.Fatalf("submit contexts: %#v", session.contexts)
	}
}

func mustRuntimeCompleted(t *testing.T) protocol.Event {
	t.Helper()
	usage, err := domain.NewSampleUsage(
		domain.KnownUsageMetric(8),
		domain.KnownUsageMetric(2),
		domain.UnknownUsageMetric(),
		domain.KnownUsageMetric(5),
		domain.NotApplicableUsageMetric(),
	)
	if err != nil {
		t.Fatal(err)
	}
	event, err := protocol.NewTurnCompleted(usage)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestModelQueuesInterruptUntilSubmitIsReady(t *testing.T) {
	session := &fakeChatSession{events: make(chan protocol.Event, 1)}
	session.events <- protocol.NewTurnStarted()
	model := NewModel(context.Background(), "test", session)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	updated, submitCommand := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)

	updated, interruptCommand := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if interruptCommand != nil || !model.interruptNext {
		t.Fatalf("interrupt was not queued: %#v", model)
	}

	model = updateModel(t, model, testAdmissionMessage(model, submitCommand))
	item := <-model.outputs
	next, interruptCommand := model.projectControlItem(item)
	model = next.(Model)
	if interruptCommand == nil {
		t.Fatal("turn start did not deliver pending stop")
	}
	interruptCommand()

	if session.interrupts != 1 || model.interruptNext {
		t.Fatalf("queued interrupt result: interrupts=%d model=%#v", session.interrupts, model)
	}
}

func updateModel(t *testing.T, model Model, message tea.Msg) Model {
	t.Helper()
	updated, _ := model.Update(message)
	return updated.(Model)
}

func updateModelWithCommand(t *testing.T, model Model, message tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, command := model.Update(message)
	return updated.(Model), command
}

func testCommitAcceptedInput(t *testing.T, model Model) Model {
	t.Helper()
	input := model.inputs[0]
	event := protocol.NewTurnStarted()
	event.TurnID = "018f1d8a-7b5c-7def-8234-456789abcdef"
	model = updateModel(t, model, RuntimeEventMsg{Event: event})
	value, err := domain.NewUserInput(input.id, input.text)
	if err != nil {
		t.Fatal(err)
	}
	event, err = protocol.NewUserInputCommitted(value)
	if err != nil {
		t.Fatal(err)
	}
	event.TurnID = model.activeTurnID
	model = updateModel(t, model, RuntimeEventMsg{Event: event})
	status, _ := protocol.NewInputStatus("ui-input", input.id, protocol.InputCommitted, model.activeTurnID, "")
	item, _ := protocol.NewInputStatusItem(status)
	return updateModel(t, model, item)
}
