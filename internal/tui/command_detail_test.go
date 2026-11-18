package tui

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
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
	model.outputs = make(chan protocol.ControlItem)
	model.composer = composerWithText(t, "kept draft")
	model = updateModel(t, model, RuntimeEventMsg{Event: approval})
	resolved, err := protocol.NewToolApprovalResolved(protocol.ToolApprovalResolvedPayload{RequestID: request.RequestID, InvocationID: request.InvocationID, Fingerprint: request.Fingerprint, Decision: protocol.ApprovalAllowOnce})
	if err != nil {
		t.Fatal(err)
	}
	resolved.TurnID = approval.TurnID
	model = updateModel(t, model, RuntimeEventMsg{Event: resolved})
	activity := protocol.ToolActivityPayload{Tool: protocol.ToolShell, InvocationID: request.InvocationID, Summary: "printf 'full command first line' …", Workdir: "src", Status: "running"}
	started, err := protocol.NewToolStarted(activity)
	if err != nil {
		t.Fatal(err)
	}
	started.TurnID = approval.TurnID
	model = updateModel(t, model, RuntimeEventMsg{Event: started})
	activity.ElapsedMillis, activity.StdoutBytes, activity.StdoutLines, activity.Recent = 250, 512, 3, "recent output\nlast line"
	progress, err := protocol.NewToolProgress(activity)
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
		completed, err := protocol.NewToolTerminal(protocol.ToolTerminalPayload{ToolActivityPayload: activity, ResultPreview: "stdout:\nhead retained\n[... 128 bytes omitted ...]\ntail retained\nstderr:\nwarning"})
		if err != nil {
			t.Fatal(err)
		}
		completed.TurnID = approval.TurnID
		model = updateModel(t, model, RuntimeEventMsg{Event: completed})
	}
	item := protocol.TranscriptListItem{Cursor: protocol.TranscriptCursor{Sequence: 1}, Kind: domain.TranscriptCall, TurnID: model.activeTurnID, Activity: activity, ModelShortened: truncated}
	preview := ""
	if terminal {
		preview = "stdout:\nhead retained\n[... 128 bytes omitted ...]\ntail retained\nstderr:\nwarning"
		item.Stdout.Status = "not_needed"
		item.Stderr.Status = "not_needed"
	}
	model = model.WithTranscriptQueries(domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef"), snapshotTranscriptQueries{item: item, input: request.Command, preview: preview})
	return model, session
}

type snapshotTranscriptQueries struct {
	item           protocol.TranscriptListItem
	input, preview string
}

func (q snapshotTranscriptQueries) ListTranscript(context.Context, protocol.TranscriptListQuery) (protocol.TranscriptListPage, error) {
	return protocol.TranscriptListPage{Items: []protocol.TranscriptListItem{q.item}}, nil
}
func (q snapshotTranscriptQueries) ReadTranscriptBody(_ context.Context, query protocol.TranscriptBodyQuery) (protocol.TranscriptBodyPage, error) {
	text := q.preview
	if query.Source == protocol.TranscriptInput {
		text = q.input
	}
	return protocol.TranscriptBodyPage{Text: text, NextOffset: uint64(len(text)), TotalBytes: uint64(len(text))}, nil
}
func (snapshotTranscriptQueries) ReadToolOutput(context.Context, protocol.ToolOutputQuery) (protocol.ToolOutputPage, error) {
	return protocol.ToolOutputPage{}, nil
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
	detail := model.toolActivities[snapshotCommandID(model)]
	detail.activity.Status, detail.activity.Code = "cancelled", "cancelled"
	terminal, err := protocol.NewToolTerminal(protocol.ToolTerminalPayload{ToolActivityPayload: detail.activity, ResultPreview: "Exec cancelled"})
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

func snapshotCommandID(model Model) string {
	return model.browser.queries.(snapshotTranscriptQueries).item.Activity.InvocationID
}
