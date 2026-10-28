package tui

import (
	"context"
	"os"
	"path/filepath"
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

func TestCommandTimelineOrderingAndTerminalEscapes(t *testing.T) {
	model := NewModel(context.Background(), "test", &fakeChatSession{})
	turnID := mustExecApproval(t).TurnID
	model.activeTurnID, model.state = turnID, stateStreaming
	payload := protocol.CommandActivityPayload{InvocationID: eventApprovalInvocationID, Command: "printf hello", Workdir: "src", Status: "running"}
	started, err := protocol.NewCommandStarted(payload)
	if err != nil {
		t.Fatal(err)
	}
	started.TurnID = turnID
	model = updateModel(t, model, RuntimeEventMsg{Event: started})
	if len(model.transcript) != 1 || !strings.Contains(model.transcript[0].text, "Running") {
		t.Fatal("missing running command")
	}
	payload.StdoutBytes, payload.StdoutLines, payload.ElapsedMillis = 20, 1, 100
	payload.Recent = "hello\n\x1b]52;c;secret\a\u202e"
	progress, err := protocol.NewCommandProgress(payload)
	if err != nil {
		t.Fatal(err)
	}
	progress.TurnID = turnID
	model = updateModel(t, model, RuntimeEventMsg{Event: progress})
	text := model.transcript[0].text
	if strings.ContainsAny(text, "\x1b\a\u202e") || !strings.Contains(text, "100 ms · 20 bytes") || !strings.Contains(text, "hello") {
		t.Fatalf("unsafe or incomplete projection: %q", text)
	}
	payload.Status, payload.Code, payload.ExitKnown, payload.ExitCode = "success", "ok", true, 2
	terminal, err := protocol.NewCommandTerminal(protocol.CommandTerminalPayload{CommandActivityPayload: payload, ResultPreview: "frozen result"})
	if err != nil {
		t.Fatal(err)
	}
	terminal.TurnID = turnID
	model = updateModel(t, model, RuntimeEventMsg{Event: terminal})
	frozen := model.transcript[0].text
	if !strings.Contains(frozen, "exit=2") || model.state != stateStreaming {
		t.Fatal("command completion prematurely ended turn")
	}
	for _, invalid := range []protocol.Event{progress, terminal, started} {
		model = updateModel(t, model, RuntimeEventMsg{Event: invalid})
		if model.transcript[0].text != frozen || model.errorSummary == "" {
			t.Fatal("late activity changed terminal")
		}
	}
}

func TestCommandActivityCannotPrecedeStartOrCrossTurns(t *testing.T) {
	for _, wrongTurn := range []bool{false, true} {
		model := NewModel(context.Background(), "test", &fakeChatSession{})
		model.activeTurnID = mustExecApproval(t).TurnID
		payload := protocol.CommandActivityPayload{InvocationID: eventApprovalInvocationID, Command: "true", Status: "running"}
		event, err := protocol.NewCommandProgress(payload)
		if err != nil {
			t.Fatal(err)
		}
		if !wrongTurn {
			event.TurnID = model.activeTurnID
		}
		model = updateModel(t, model, RuntimeEventMsg{Event: event})
		if len(model.transcript) != 0 || model.errorSummary == "" {
			t.Fatal("out of order command admitted")
		}
	}
}

func TestExecApprovalSnapshotsAndViewportState(t *testing.T) {
	for _, test := range []struct {
		name                  string
		width, height, scroll int
	}{
		{"command_approval_normal", 80, 24, 0},
		{"command_approval_small", 46, 20, 0},
		{"command_approval_scroll", 46, 20, 8},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := NewModel(context.Background(), "test", &fakeChatSession{})
			event := mustExecApproval(t)
			payload, err := protocol.DecodeToolApprovalRequested(event)
			if err != nil {
				t.Fatal(err)
			}
			payload.Command = strings.Repeat("printf 'long command line'\n", 12) + "printf done"
			event, err = protocol.NewToolApprovalRequested(payload)
			if err != nil {
				t.Fatal(err)
			}
			event.TurnID = mustExecApproval(t).TurnID
			model = updateModel(t, model, RuntimeEventMsg{Event: event})
			model = updateModel(t, model, tea.WindowSizeMsg{Width: test.width, Height: test.height})
			for range test.scroll {
				model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyPgDown})
			}
			got := model.View()
			if strings.Count(got, "\n") > test.height {
				t.Fatalf("approval exceeds viewport:\n%s", got)
			}
			for _, required := range []string{"Workdir: src", "Timeout: 1000 ms", "> No", "Abort turn", "Command "} {
				if !strings.Contains(got, required) {
					t.Fatalf("missing persistent metadata %q", required)
				}
			}
			if model.approval.request.Command != payload.Command || model.approval.selected != 1 {
				t.Fatal("scroll changed approval candidate or default")
			}
			want, err := os.ReadFile(filepath.Join("testdata", test.name+".golden"))
			if err != nil {
				t.Fatalf("snapshot %s missing; actual=%q", test.name, got)
			}
			if got != string(want) {
				t.Fatalf("snapshot mismatch:\n%s", got)
			}
			model = updateModel(t, model, tea.WindowSizeMsg{Width: 100, Height: 30})
			if model.approval.request.Command != payload.Command || model.approval.selected != 1 || model.approval.scroll != test.scroll {
				t.Fatal("resize mutated candidate or navigation state")
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
