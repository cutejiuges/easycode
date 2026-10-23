package tui

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

type fakeChatSession struct {
	mu         sync.Mutex
	submits    []string
	events     chan protocol.Event
	submitErr  error
	interrupts int
	contexts   []context.Context
	approvals  []capturedApproval
}

type capturedApproval struct {
	turnID   domain.TurnID
	request  protocol.ToolApprovalRequestedPayload
	decision protocol.ApprovalDecision
	feedback string
}

func (session *fakeChatSession) Submit(ctx context.Context, text string) (<-chan protocol.Event, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.contexts = append(session.contexts, ctx)
	session.submits = append(session.submits, text)
	if session.submitErr != nil {
		return nil, session.submitErr
	}
	if session.events == nil {
		session.events = make(chan protocol.Event, 8)
	}
	return session.events, nil
}

func (session *fakeChatSession) Interrupt() {
	session.mu.Lock()
	session.interrupts++
	session.mu.Unlock()
}

func (session *fakeChatSession) ResolveApproval(_ context.Context, turnID domain.TurnID, request protocol.ToolApprovalRequestedPayload, decision protocol.ApprovalDecision, feedback string) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.approvals = append(session.approvals, capturedApproval{turnID: turnID, request: request, decision: decision, feedback: feedback})
	return nil
}

func TestApprovalOverlayPreservesDraftAndSubmitsCorrelatedDecision(t *testing.T) {
	session := &fakeChatSession{}
	model := NewModel(context.Background(), "test", session)
	model.draft = []rune("kept draft")
	event := mustApprovalRequested(t)
	model = updateModel(t, model, RuntimeEventMsg{Event: event})
	if model.approval == nil || model.approval.selected != 2 {
		t.Fatalf("approval overlay = %#v", model.approval)
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ignored")})
	if string(model.draft) != "kept draft" {
		t.Fatalf("draft changed under overlay: %q", string(model.draft))
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyUp})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyUp})
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if command == nil || !model.approval.submitted {
		t.Fatal("approval was not submitted")
	}
	_ = command()
	if len(session.approvals) != 1 || session.approvals[0].decision != protocol.ApprovalAllowOnce || session.approvals[0].turnID != event.TurnID || session.approvals[0].request.Fingerprint != strings.Repeat("a", 64) {
		t.Fatalf("approval = %#v", session.approvals)
	}
	_, duplicate := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if duplicate != nil {
		t.Fatal("submitted overlay accepted a duplicate decision")
	}
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

func TestApprovalScrollAndResizeKeepCandidateState(t *testing.T) {
	model := NewModel(context.Background(), "test", &fakeChatSession{})
	event := mustApprovalRequested(t)
	payload, _ := protocol.DecodeToolApprovalRequested(event)
	payload.Presentation.Patch.Diff = strings.Repeat("-old line\n+new line\n", 30)
	event, _ = protocol.NewToolApprovalRequested(payload)
	event.TurnID, _ = domain.ParseTurnID("018f1d8a-7b5c-7def-8234-456789abcdef")
	model = updateModel(t, model, RuntimeEventMsg{Event: event})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("保留反馈")})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	for range 5 {
		model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	before := *model.approval
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 32, Height: 10})
	if model.approval.request.Fingerprint != before.request.Fingerprint || model.approval.selected != before.selected || string(model.approval.feedback) != string(before.feedback) || model.approval.scroll != before.scroll {
		t.Fatalf("resize changed approval semantics: before=%#v after=%#v", before, model.approval)
	}
	view := model.View()
	if !strings.Contains(view, "of 61") || !strings.Contains(view, "No") || strings.Contains(view, "kept draft") {
		t.Fatalf("small approval view = %q", view)
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
		Presentation: protocol.ApprovalPresentation{Kind: protocol.PresentationPatch, Patch: &protocol.PatchApprovalPresentation{Diff: "--- a/src/main.go\n+++ b/src/main.go\n-old\n+new\n"}},
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
	if command == nil || model.state != stateStreaming || len(model.transcript) != 1 {
		t.Fatalf("submit state: %#v", model)
	}
	ready := command()
	model, command = updateModelWithCommand(t, model, ready)
	if command == nil {
		t.Fatal("missing event wait command")
	}

	first, _ := protocol.NewAssistantTextDelta("hel")
	second, _ := protocol.NewAssistantTextDelta("lo")
	session.events <- first
	model, command = updateModelWithCommand(t, model, command())
	session.events <- second
	model, command = updateModelWithCommand(t, model, command())
	session.events <- mustRuntimeCompleted(t)
	model, command = updateModelWithCommand(t, model, command())

	if command != nil || model.state != stateIdle {
		t.Fatalf("completed state: %#v command=%v", model, command)
	}
	if len(model.transcript) != 2 || model.transcript[1].text != "hello" {
		t.Fatalf("transcript: %#v", model.transcript)
	}
	if len(session.submits) != 1 || session.submits[0] != "hello" {
		t.Fatalf("submits: %#v", session.submits)
	}
	if len(session.contexts) != 1 || session.contexts[0] != submitContext {
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

func TestModelInputAndCancellationKeys(t *testing.T) {
	session := &fakeChatSession{}
	model := NewModel(context.Background(), "test", session)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if string(model.draft) != "q" {
		t.Fatalf("q is not draft input: %q", string(model.draft))
	}

	empty := NewModel(context.Background(), "test", session)
	updated, command := empty.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || len(updated.(Model).transcript) != 0 {
		t.Fatal("empty input was submitted")
	}

	model.state = stateStreaming
	model.events = make(chan protocol.Event)
	before := len(session.submits)
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || len(session.submits) != before || updated.(Model).state != stateStreaming {
		t.Fatal("streaming enter started another turn")
	}
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if command == nil {
		t.Fatal("escape did not create interrupt command")
	}
	_ = command()
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_ = command()
	if session.interrupts != 2 {
		t.Fatalf("interrupt count: %d", session.interrupts)
	}

	idle := NewModel(context.Background(), "test", session)
	_, command = idle.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if command == nil {
		t.Fatal("idle empty Ctrl+C did not quit")
	}
	if message := command(); message != tea.Quit() {
		t.Fatalf("quit message: %#v", message)
	}
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

	model, interruptCommand = updateModelWithCommand(t, model, submitCommand())
	batch, ok := interruptCommand().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("queued commands: %#v", batch)
	}
	for _, command := range batch {
		_ = command()
	}
	if session.interrupts != 1 || model.interruptNext {
		t.Fatalf("queued interrupt result: interrupts=%d model=%#v", session.interrupts, model)
	}
}

func TestModelFailureAndCancellationRemainRecoverable(t *testing.T) {
	for _, test := range []struct {
		name      string
		code      fault.Code
		cancelled bool
	}{
		{name: "failed", code: fault.CodeProviderRequest},
		{name: "cancelled", code: fault.CodeUserCancelled, cancelled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &fakeChatSession{}
			model := NewModel(context.Background(), "test", session)
			model.transcript = []transcriptMessage{{role: "User", text: "old"}, {role: "Assistant", text: "kept"}}
			model.state = stateStreaming
			model.assistantItem = 1
			failure, err := protocol.NewTurnFailed(string(test.code), "safe failure", test.cancelled)
			if err != nil {
				t.Fatalf("new failure: %v", err)
			}
			model = updateModel(t, model, RuntimeEventMsg{Event: failure})
			if model.state != stateIdle || model.transcript[1].text != "kept" || !strings.Contains(model.errorSummary, string(test.code)) {
				t.Fatalf("failure state: %#v", model)
			}
			model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("retry")})
			updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if command == nil || updated.(Model).state != stateStreaming {
				t.Fatal("model did not recover for retry")
			}
		})
	}
}

func TestModelSubmitErrorIsSanitized(t *testing.T) {
	session := &fakeChatSession{submitErr: fault.New(fault.CodeProviderRequest, "provider request failed")}
	model := NewModel(context.Background(), "test", session)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	model = updateModel(t, model, command())
	if model.errorSummary != "provider_request_failed: provider request failed" || model.state != stateIdle {
		t.Fatalf("submit error: %#v", model)
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
