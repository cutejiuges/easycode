package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func commandDetailModel(t *testing.T, terminal bool, status, code string, truncated bool) (Model, *fakeChatSession) {
	t.Helper()
	session := &fakeChatSession{}
	model := NewModel(context.Background(), "test", session)
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	approval := mustExecApproval(t)
	request, err := protocol.DecodeToolApprovalRequested(approval)
	if err != nil {
		t.Fatal(err)
	}
	request.Command = "printf 'full command first line'\nprintf 'full command second line'"
	approval, err = protocol.NewToolApprovalRequested(request)
	if err != nil {
		t.Fatal(err)
	}
	approval.TurnID = mustExecApproval(t).TurnID
	model.activeTurnID, model.state = approval.TurnID, stateStreaming
	model.events = make(chan protocol.Event)
	model.composer = composerWithText(t, "kept draft")
	model = updateModel(t, model, RuntimeEventMsg{Event: approval})
	resolved, err := protocol.NewToolApprovalResolved(protocol.ToolApprovalResolvedPayload{RequestID: request.RequestID, InvocationID: request.InvocationID, Fingerprint: request.Fingerprint, Decision: protocol.ApprovalAllowOnce})
	if err != nil {
		t.Fatal(err)
	}
	resolved.TurnID = approval.TurnID
	model = updateModel(t, model, RuntimeEventMsg{Event: resolved})
	activity := protocol.CommandActivityPayload{InvocationID: request.InvocationID, Command: "printf 'full command first line' …", Workdir: "src", Status: "running"}
	started, err := protocol.NewCommandStarted(activity)
	if err != nil {
		t.Fatal(err)
	}
	started.TurnID = approval.TurnID
	model = updateModel(t, model, RuntimeEventMsg{Event: started})
	activity.ElapsedMillis, activity.StdoutBytes, activity.StdoutLines, activity.Recent = 250, 512, 3, "recent output\nlast line"
	progress, err := protocol.NewCommandProgress(activity)
	if err != nil {
		t.Fatal(err)
	}
	progress.TurnID = approval.TurnID
	model = updateModel(t, model, RuntimeEventMsg{Event: progress})
	if terminal {
		activity.Status, activity.Code, activity.Truncated = status, code, truncated
		if status == "success" {
			activity.ExitKnown, activity.ExitCode = true, 2
		}
		completed, err := protocol.NewCommandTerminal(protocol.CommandTerminalPayload{CommandActivityPayload: activity, ResultPreview: "stdout:\nhead retained\n[... 128 bytes omitted ...]\ntail retained\nstderr:\nwarning"})
		if err != nil {
			t.Fatal(err)
		}
		completed.TurnID = approval.TurnID
		model = updateModel(t, model, RuntimeEventMsg{Event: completed})
	}
	return model, session
}

func TestCommandTimelineAndDetailSnapshots(t *testing.T) {
	for _, test := range []struct {
		name, status, code          string
		terminal, truncated, detail bool
		width, height, scroll       int
	}{
		{name: "command_running", width: 80, height: 24},
		{name: "command_nonzero", terminal: true, status: "success", code: "ok", width: 80, height: 24},
		{name: "command_timeout", terminal: true, status: "error", code: "timed_out", width: 80, height: 24},
		{name: "command_cancelled", terminal: true, status: "cancelled", code: "cancelled", width: 80, height: 24},
		{name: "command_truncated", terminal: true, status: "success", code: "ok", truncated: true, width: 80, height: 24},
		{name: "command_detail_running", detail: true, width: 80, height: 24},
		{name: "command_detail_complete", terminal: true, status: "success", code: "ok", truncated: true, detail: true, width: 80, height: 24},
		{name: "command_detail_small", terminal: true, status: "success", code: "ok", truncated: true, detail: true, width: 42, height: 14, scroll: 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, _ := commandDetailModel(t, test.terminal, test.status, test.code, test.truncated)
			model = updateModel(t, model, tea.WindowSizeMsg{Width: test.width, Height: test.height})
			if test.detail {
				model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyCtrlO})
			}
			for range test.scroll {
				model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyPgDown})
			}
			got := model.View()
			if test.detail && strings.Count(got, "\n") > test.height {
				t.Fatal("detail exceeds viewport")
			}
			want, err := os.ReadFile(filepath.Join("testdata", test.name+".golden"))
			if err != nil {
				t.Fatalf("snapshot %s missing; actual=%q", test.name, got)
			}
			if got != string(want) {
				t.Fatalf("snapshot mismatch:\n%s", got)
			}
		})
	}
}

func TestCommandDetailPreservesCandidateDraftAndInterruptRouting(t *testing.T) {
	model, session := commandDetailModel(t, false, "", "", false)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyCtrlO})
	stored := model.commandViews[model.latestCommand]
	if !strings.Contains(model.View(), "full command second line") || strings.Contains(model.View(), "head retained") {
		t.Fatal("running details did not use approval command and recent output")
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("ignored")}, {Type: tea.KeyEnter}, {Type: tea.KeyPgDown}} {
		model = updateModel(t, model, key)
	}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 42, Height: 14})
	if model.commandViews[model.latestCommand] != stored || model.composer.text != "kept draft" || len(session.submits) != 0 {
		t.Fatal("detail navigation changed candidate or composer")
	}
	updated, interrupt := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = updated.(Model)
	if interrupt == nil {
		t.Fatal("detail swallowed interrupt")
	}
	interrupt()
	if session.interrupts != 1 || model.state != stateStreaming {
		t.Fatal("interrupt prematurely released turn")
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.detailOpen || session.interrupts != 1 {
		t.Fatal("Escape did not only close details")
	}
	completed, _ := commandDetailModel(t, true, "success", "ok", true)
	completed = updateModel(t, completed, tea.KeyMsg{Type: tea.KeyCtrlO})
	if !strings.Contains(completed.View(), "head retained") || !strings.Contains(completed.View(), "tail retained") {
		t.Fatal("completed detail did not use frozen preview")
	}
	frozen := completed.commandViews[completed.latestCommand]
	completed = updateModel(t, completed, tea.KeyMsg{Type: tea.KeyCtrlO})
	if completed.detailOpen || completed.commandViews[completed.latestCommand] != frozen {
		t.Fatal("closing details changed result")
	}
}

func TestCompactCommandSummaryKeepsTwoLinesAndCodePointBudget(t *testing.T) {
	for _, width := range []int{20, 42, 80, 100, 180} {
		summary := commandSummary(strings.Repeat("界", 200), width)
		if strings.Count(summary, "\n") > 1 || utf8.RuneCountInString(summary) > 160 {
			t.Fatalf("unbounded summary at width %d", width)
		}
	}
}

func TestCommandInterruptKeepsDraftBlockedUntilTurnCleanupTerminal(t *testing.T) {
	model, session := commandDetailModel(t, false, "", "", false)
	updated, interrupt := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if interrupt == nil {
		t.Fatal("missing interrupt")
	}
	interrupt()
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyRunes, Runes: []rune("ignored")}} {
		model = updateModel(t, model, key)
	}
	if model.state != stateStreaming || model.composer.text != "kept draft" || len(session.submits) != 0 || !strings.Contains(model.View(), "waiting for cleanup") {
		t.Fatal("cancellation released composer before cleanup")
	}
	detail := model.commandViews[model.latestCommand]
	detail.activity.Status, detail.activity.Code = "cancelled", "cancelled"
	terminal, err := protocol.NewCommandTerminal(protocol.CommandTerminalPayload{CommandActivityPayload: detail.activity, ResultPreview: "Exec cancelled"})
	if err != nil {
		t.Fatal(err)
	}
	terminal.TurnID = model.activeTurnID
	model = updateModel(t, model, RuntimeEventMsg{Event: terminal})
	if model.state != stateStreaming {
		t.Fatal("command terminal ended outer turn")
	}
	failure, err := protocol.NewTurnFailed("user_cancelled", "turn cancelled", true)
	if err != nil {
		t.Fatal(err)
	}
	failure.TurnID = model.activeTurnID
	model = updateModel(t, model, RuntimeEventMsg{Event: failure})
	if model.state != stateIdle || model.cancelling || model.composer.text != "kept draft" {
		t.Fatal("turn cleanup did not restore composer")
	}
	_, submit := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if submit == nil {
		t.Fatal("composer remained blocked after cleanup")
	}
	submit()
	if len(session.submits) != 1 || session.submits[0] != "kept draft" {
		t.Fatal("completed cleanup lost draft")
	}
}
