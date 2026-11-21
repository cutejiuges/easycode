package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

type workModeFixture struct {
	fakeChatSession
	requests []protocol.Command
}

func (s *workModeFixture) SetWorkMode(ctx context.Context, id protocol.RequestID, mode domain.WorkMode) (protocol.CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.CommandResult{}, err
	}
	command, err := protocol.NewSetWorkModeCommand(id, mode)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	s.requests = append(s.requests, command)
	return protocol.NewAcceptedCommandResult(command, protocol.CommandDispositionUpdated)
}

func workModeModel(thread domain.ThreadID) (Model, *workModeFixture) {
	session := &workModeFixture{}
	return NewModel(context.Background(), "test", session).WithTaskProgressScope(domain.SessionID(thread), thread), session
}

func workModeItem(t *testing.T, id protocol.RequestID, thread domain.ThreadID, mode domain.WorkMode) protocol.ControlItem {
	t.Helper()
	event, err := protocol.NewWorkModeSnapshot(domain.SessionID(thread), thread, mode)
	if err != nil {
		t.Fatal(err)
	}
	item, err := protocol.NewCorrelatedContextItem(id, event)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestPlanCommandCompletionUsesOneLocalDecision(t *testing.T) {
	model, session := workModeModel(resumeThreadA)
	files := &fileQueryFixture{results: make(chan domain.FileSearchSnapshot, 1)}
	model = model.WithSkills([]domain.SkillListEntry{completionSkillEntry(t, "plan", "Planning skill")}).WithFileQueries(files)
	model.composer = composerWithText(t, "/pla")
	model, cmd := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil || model.composer.text != "/plan " || model.workMode.pending != nil || model.skillInvocationText("plan") != "$plan " {
		t.Fatal("Tab changed mode or selected the colliding Skill")
	}
	model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || model.workMode.mode != domain.WorkModeDefault || model.composer.text != "/plan " {
		t.Fatal("submission cleared the draft or changed the mode ahead of confirmation")
	}
	if message := cmd(); message != nil || len(session.requests) != 1 || session.requests[0].WorkMode() != domain.WorkModePlan {
		t.Fatal("local command did not submit exactly one explicit Plan request")
	}
	accepted, _ := protocol.NewAcceptedCommandResult(session.requests[0], protocol.CommandDispositionUpdated)
	item, _ := protocol.NewCommandResultItem(accepted)
	model = updateModel(t, model, item)
	if model.workMode.mode != domain.WorkModeDefault || model.workMode.pending == nil {
		t.Fatal("command result was treated as authoritative mode")
	}
	id := session.requests[0].RequestID()
	model = updateModel(t, model, workModeItem(t, id, resumeThreadB, domain.WorkModePlan))
	if model.workMode.pending == nil || model.workMode.mode != domain.WorkModeDefault {
		t.Fatal("another thread's confirmation took effect")
	}
	model = updateModel(t, model, workModeItem(t, id, resumeThreadA, domain.WorkModePlan))
	if model.workMode.pending != nil || model.composer.text != "" || len(model.composer.history.entries) != 0 ||
		len(session.submits) != 0 || !strings.Contains(model.View(), "Plan mode · read-only") {
		t.Fatal("confirmation did not consume just the local command")
	}

	for _, draft := range []string{"/plan @src", "/plan\noff"} {
		model.composer = composerWithText(t, draft)
		model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil || len(files.queries) != 0 || model.workMode.pending != nil ||
			model.composer.text != draft || !strings.HasPrefix(model.errorSummary, "invalid_command:") {
			t.Fatalf("invalid Plan command was not local: %q", draft)
		}
	}
	model.composer = composerWithText(t, "$plan @src")
	model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || len(files.queries) != 1 || model.workMode.pending != nil {
		t.Fatal("same-name Skill did not retain file and submit routing")
	}
	cmd()
	if !reflect.DeepEqual(session.submits, []string{"$plan @src"}) {
		t.Fatal("same-name Skill text did not reach normal admission")
	}
}

func TestPlanPendingFreezesDraftAndCancelsOnlyMode(t *testing.T) {
	model, session := workModeModel(resumeThreadA)
	model.resumeEnabled = true
	model.workMode.mode = domain.WorkModePlan
	model.composer = composerWithText(t, "/pla off")
	model.composer.history.add("previous user input")
	model.composer.moveTo(len("/pla"))
	before := model.composer
	model, cmd := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || model.workMode.pending == nil {
		t.Fatal("completion did not send the local request")
	}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 30, Height: 7})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/resume"), Paste: true})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyCtrlC})
	if model.resumeIntent != nil || model.workMode.pending == nil || model.interruptNext || model.quitRequested {
		t.Fatal("a pending control allowed another action")
	}
	model = updateModel(t, model, cmd())
	if len(session.requests) != 0 || model.workMode.pending != nil ||
		model.workMode.mode != domain.WorkModePlan || model.composer.text != before.text ||
		model.composer.cursor != before.cursor || model.composer.width != 30 ||
		!reflect.DeepEqual(model.composer.history, before.history) {
		t.Fatal("early cancellation lost the pre-completion draft or changed mode")
	}

	model.restoreComposer(composerWithText(t, "/plan off"))
	retry := model.composer
	model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || cmd() != nil || len(session.requests) != 1 {
		t.Fatal("busy operation fixture could not send a mode request")
	}
	rejected, _ := protocol.NewRejectedCommandResult(session.requests[0], protocol.ControlErrorSessionBusy)
	item, _ := protocol.NewCommandResultItem(rejected)
	model = updateModel(t, model, item)
	if model.workMode.pending != nil || model.workMode.mode != domain.WorkModePlan ||
		model.composer.text != retry.text || model.composer.cursor != retry.cursor || !strings.HasPrefix(model.errorSummary, "session_busy:") {
		t.Fatal("owner rejection consumed the draft or replaced the mode")
	}

	model.composer = composerWithText(t, "new draft")
	model.composer.moveTo(3)
	model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd == nil || cmd() != nil || len(session.requests) != 2 {
		t.Fatal("Shift+Tab did not send the target")
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	model = updateModel(t, model, workModeItem(t, session.requests[1].RequestID(), resumeThreadA, domain.WorkModeDefault))
	if model.workMode.mode != domain.WorkModeDefault || model.workMode.pending != nil || model.interruptNext || session.interrupts != 0 ||
		model.composer.text != "new draft" || model.composer.cursor != 3 || !strings.Contains(model.View(), "Default") {
		t.Fatal("late confirmation did not keep the exact editable draft")
	}
	model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("mode cancellation blocked the next normal input")
	}
	model = updateModel(t, model, testAdmissionMessage(model, cmd))
	started := protocol.NewTurnStarted()
	started.TurnID = "01900000-0000-7000-8000-000000000010"
	model = updateModel(t, model, RuntimeEventMsg{Event: started})
	if model.cancelling || model.interruptNext || session.interrupts != 0 || !reflect.DeepEqual(session.submits, []string{"new draft"}) {
		t.Fatal("the next turn inherited mode cancellation")
	}
}

func TestPlanFocusAndConversationGeneration(t *testing.T) {
	model, _ := workModeModel(resumeThreadA)
	model.workMode.mode = domain.WorkModePlan
	model = updateModel(t, model, RuntimeEventMsg{Event: questionUIEvent(t, 2)})
	model.question.current = 1
	model, cmd := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd != nil || model.question.current != 0 || model.workMode.pending != nil || !strings.Contains(model.View(), "Question") {
		t.Fatal("mode control took a question key or displaced its view")
	}

	old, _ := workModeModel(resumeThreadA)
	old.workMode.mode = domain.WorkModePlan
	old.browser.thread = resumeThreadA
	shell, _ := resumeTestShell(old)
	generation := shell.generation
	shell = resumeDirect(shell, resumeThreadB)
	shell = pickerUpdate(shell, PickerResult{Request: shell.request, Stage: PickerCancelled})
	if shell.chat.workMode.mode != domain.WorkModePlan || !strings.Contains(shell.View(), "Plan mode · read-only") {
		t.Fatal("cancelled handoff lost Plan")
	}
	fresh, _ := workModeModel(resumeThreadA)
	shell.mount(fresh, true)
	if !strings.Contains(shell.View(), "Default") {
		t.Fatal("newly attached conversation did not expose Default before submission")
	}
	shell = pickerKey(shell, tea.KeyShiftTab)
	id := shell.chat.workMode.pending.id
	shell = pickerUpdate(shell, scopedChatMessage{generation: generation, value: workModeItem(t, id, resumeThreadA, domain.WorkModePlan)})
	if shell.chat.workMode.mode != domain.WorkModeDefault || shell.chat.workMode.pending == nil {
		t.Fatal("old host confirmation took effect")
	}
	shell = pickerUpdate(shell, scopedChatMessage{generation: shell.generation, value: eventStreamClosedMsg{}})
	if shell.chat.workMode.pending != nil {
		t.Fatal("control stream closure left mode admission pending")
	}
}
