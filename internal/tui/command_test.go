package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/protocol"
)

func mustExecApproval(t *testing.T) protocol.Event {
	t.Helper()
	event := mustApprovalRequested(t)
	payload, err := protocol.DecodeToolApprovalRequested(event)
	if err != nil {
		t.Fatal(err)
	}
	payload.Capability, payload.ProviderCallID = "process.exec", "call-exec"
	payload.Presentation, payload.Command = nil, "printf hello"
	payload.RelativePath, payload.TimeoutMillis = "src", 1000
	payload.Reason = "Foreground only; detached processes may continue running."
	payload.Decisions = []protocol.ApprovalDecision{protocol.ApprovalAllowOnce, protocol.ApprovalDeny, protocol.ApprovalAbort}
	result, err := protocol.NewToolApprovalRequested(payload)
	if err != nil {
		t.Fatal(err)
	}
	result.TurnID = event.TurnID
	return result
}

func TestExecApprovalDefaultNoAndExactDecisionRouting(t *testing.T) {
	for _, test := range []struct {
		name     string
		key      tea.KeyType
		decision protocol.ApprovalDecision
	}{
		{"deny", tea.KeyTab, protocol.ApprovalDeny},
		{"allow", tea.KeyUp, protocol.ApprovalAllowOnce},
		{"abort", tea.KeyDown, protocol.ApprovalAbort},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &fakeChatSession{}
			model := NewModel(context.Background(), "test", session)
			model.composer = composerWithText(t, "keep draft")
			event := mustExecApproval(t)
			model = updateModel(t, model, RuntimeEventMsg{Event: event})
			if model.approval == nil || model.approval.selected != 1 {
				t.Fatal("Exec must default to No")
			}
			model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ignored")})
			if model.composer.text != "keep draft" {
				t.Fatal("modal changed composer")
			}
			model = updateModel(t, model, tea.KeyMsg{Type: test.key})
			updated, submit := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			model = updated.(Model)
			if submit == nil {
				t.Fatal("missing approval submission")
			}
			submit()
			if len(session.approvals) != 1 || session.approvals[0].decision != test.decision || session.approvals[0].request.Command != "printf hello" {
				t.Fatalf("approval = %#v", session.approvals)
			}
			_, repeat := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if repeat != nil || len(session.submits) != 0 {
				t.Fatal("modal admitted duplicate approval or turn")
			}
		})
	}
}

func TestExecApprovalRejectsStaleResolution(t *testing.T) {
	model := NewModel(context.Background(), "test", &fakeChatSession{})
	event := mustExecApproval(t)
	model = updateModel(t, model, RuntimeEventMsg{Event: event})
	for _, field := range []string{"turn", "invocation", "fingerprint"} {
		payload := protocol.ToolApprovalResolvedPayload{RequestID: eventApprovalRequestID, InvocationID: eventApprovalInvocationID, Fingerprint: strings.Repeat("a", 64), Decision: protocol.ApprovalDeny}
		if field == "invocation" {
			payload.InvocationID = eventApprovalRequestID
		}
		if field == "fingerprint" {
			payload.Fingerprint = strings.Repeat("b", 64)
		}
		resolved, err := protocol.NewToolApprovalResolved(payload)
		if err != nil {
			t.Fatal(err)
		}
		if field != "turn" {
			resolved.TurnID = event.TurnID
		}
		model = updateModel(t, model, RuntimeEventMsg{Event: resolved})
		if model.approval == nil || model.errorSummary == "" {
			t.Fatalf("stale %s closed current approval", field)
		}
	}
}
