package tui

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func TestPromptSubmitHookReviewAndReturnedInput(t *testing.T) {
	const session = domain.SessionID("01900000-0000-7000-8000-000000000051")
	const thread = domain.ThreadID("01900000-0000-7000-8000-000000000052")
	const turn = domain.TurnID("01900000-0000-7000-8000-000000000053")
	const input = domain.InputID("01900000-0000-7000-8000-000000000054")
	const requestID protocol.RequestID = "submit-1"
	declaration, err := domain.NewUserPromptSubmitHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, domain.HookDigest(nil), "prompt-policy", "cat >/dev/null; exit 2", 5)
	if err != nil {
		t.Fatal(err)
	}
	target, err := domain.NewUserPromptSubmitHookTarget(domain.PromptSubmitTarget{TurnID: turn, InputID: input, SampleIndex: 0})
	if err != nil {
		t.Fatal(err)
	}
	review := protocol.HookTrustRequestedPayload{
		RequestID: "01900000-0000-7000-8000-000000000055", Target: target, Declaration: declaration, PlanDigest: domain.HookDigest([]byte("plan")),
	}
	reviewEvent, err := protocol.NewHookTrustRequested(review)
	if err != nil {
		t.Fatal(err)
	}
	scope := func(event protocol.Event) RuntimeEventMsg {
		event.SessionID, event.ThreadID, event.TurnID = session, thread, turn
		return RuntimeEventMsg{Event: event}
	}
	claimed, _ := protocol.NewInputStatus(requestID, input, protocol.InputClaimed, turn, "")
	claimedItem, _ := protocol.NewInputStatusItem(claimed)
	rejected, err := protocol.NewHookRejectedInputStatus(requestID, input, turn, domain.HookDenied, declaration.Reference.Source, declaration.Reference.ID)
	if err != nil {
		t.Fatal(err)
	}
	rejectedItem, _ := protocol.NewInputStatusItem(rejected)
	response, err := protocol.NewHookTrustResponse(session, thread, turn, review, protocol.HookTrustSession)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := protocol.NewHookTrustResolved(response)
	if err != nil {
		t.Fatal(err)
	}
	activity, err := protocol.NewHookActivity(protocol.HookActivityPayload{
		Target: target, Reference: declaration.Reference, PlanDigest: review.PlanDigest, State: protocol.HookActivityChecking, Code: domain.HookOK,
	})
	if err != nil {
		t.Fatal(err)
	}
	command, _ := protocol.NewSubmitConversationalInputCommand(requestID, "original prompt")
	accepted, _ := protocol.NewAcceptedInputResult(command, protocol.CommandDispositionStarting, input)
	acceptedItem, _ := protocol.NewCommandResultItem(accepted)
	newModel := func() (Model, *fakeHookSession) {
		host := &fakeHookSession{entries: []domain.HookListEntry{{Declaration: declaration, State: "untrusted", Code: domain.HookOK}}}
		model := NewModel(context.Background(), "test", host).WithHooks(host.entries)
		model = updateModel(t, model, tea.WindowSizeMsg{Width: 110, Height: 24})
		pending := composerWithText(t, "original prompt")
		model.pending = &pending
		model = updateModel(t, model, acceptedItem)
		model = updateModel(t, model, scope(protocol.NewTurnStarted()))
		if err := model.composer.replace(0, 0, "another draft"); err != nil {
			t.Fatal(err)
		}
		model.composer.cursor = 3
		return model, host
	}

	t.Run("private preview and explicit recall", func(t *testing.T) {
		model, host := newModel()
		model = updateModel(t, model, scope(reviewEvent))
		if model.hookTrust != nil {
			t.Fatal("unclaimed input opened a review")
		}
		model = updateModel(t, model, claimedItem)
		model = updateModel(t, model, scope(reviewEvent))
		view := model.View()
		for _, expected := range []string{"project · prompt-policy", "reads each submitted prompt as plain text", "approved implementation text", "Trust applies to later submissions", declaration.Command} {
			if !strings.Contains(view, expected) {
				t.Fatalf("review omitted %q: %s", expected, view)
			}
		}
		if strings.Contains(view, "original prompt") || model.hookTrust.trust {
			t.Fatalf("review disclosed input or preselected trust: %s", view)
		}
		model = updateModel(t, model, tea.WindowSizeMsg{Width: 19, Height: 5})
		model, selectCommand := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyUp})
		model, acceptCommand := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
		if selectCommand != nil || acceptCommand != nil || model.hookTrust.trust || !model.hookReviewUnavailable() {
			t.Fatal("narrow review allowed trust")
		}
		model = updateModel(t, model, tea.WindowSizeMsg{Width: 110, Height: 24})
		model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyUp})
		model, trustCommand := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
		if trustCommand == nil || trustCommand() != nil || len(host.decisions) != 1 || host.decisions[0] != protocol.HookTrustSession {
			t.Fatal("resized review did not authorize the selected declaration")
		}
		model = updateModel(t, model, scope(resolution))
		model = updateModel(t, model, scope(activity))
		if !strings.Contains(model.View(), "Checking submitted prompt · project:prompt-policy") {
			t.Fatal("checking is not visible")
		}
		model = updateModel(t, model, rejectedItem)
		model = updateModel(t, model, scope(activity))
		model = updateModel(t, model, scope(reviewEvent))
		if model.hookTrust != nil || model.promptHookActivity != nil || len(model.transcript) != 0 || model.composer.text != "another draft" || model.composer.cursor != 3 {
			t.Fatalf("late activity changed the returned input or its draft: %+v", model.inputs)
		}
		if !strings.Contains(model.View(), "project:prompt-policy · hook_denied · original prompt") {
			t.Fatal("returned input omitted declaration or fixed diagnostic")
		}
		model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyCtrlR})
		if len(model.inputs) != 0 || model.composer.text != "original prompt\nanother draft" || len(model.composer.history.entries) != 1 || model.composer.history.entries[0] != "original prompt" {
			t.Fatalf("recall lost either draft: %q", model.composer.text)
		}
	})

	t.Run("cancel drops review immediately", func(t *testing.T) {
		model, host := newModel()
		model = updateModel(t, model, claimedItem)
		model = updateModel(t, model, scope(reviewEvent))
		model, cancelCommand := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyCtrlC})
		if cancelCommand == nil || model.hookTrust != nil || !model.cancelling {
			t.Fatal("cancellation left the trust overlay active")
		}
		cancelCommand()
		model = updateModel(t, model, scope(resolution))
		model = updateModel(t, model, scope(activity))
		model = updateModel(t, model, scope(reviewEvent))
		if host.interrupts != 1 || model.hookTrust != nil || model.promptHookActivity != nil || model.hooks.entries[0].State != "untrusted" || model.composer.text != "another draft" {
			t.Fatal("cancelled review changed trust or draft")
		}
	})

	t.Run("local hook browser skips submission", func(t *testing.T) {
		model, host := newModel()
		if err := model.composer.replace(0, len(model.composer.text), "/hooks"); err != nil {
			t.Fatal(err)
		}
		model, command := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
		if command != nil || len(host.submits) != 0 || !model.hooks.open || model.hookTrust != nil ||
			!strings.Contains(model.View(), "project:prompt-policy · UserPromptSubmit") {
			t.Fatal("local hook browser submitted a prompt or omitted the declaration")
		}
		model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
		if !strings.Contains(model.View(), "reads each submitted prompt as plain text") {
			t.Fatal("local hook browser omitted the disclosure detail")
		}
	})
}
