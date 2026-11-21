package tui

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

const implementationEntry domain.InputID = "01900000-0000-7000-8000-000000000011"

type planReviewFixture struct {
	workModeFixture
	approved protocol.Command
}

func (s *planReviewFixture) ApprovePlan(ctx context.Context, id protocol.RequestID, ref protocol.PlanReviewRef) (protocol.CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.CommandResult{}, err
	}
	command, err := protocol.NewApprovePlanCommand(id, ref)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	s.approved = command
	return protocol.NewAcceptedInputResult(command, protocol.CommandDispositionStarting, implementationEntry)
}

func readyPlanReview(t *testing.T, markdown string) (Model, *planReviewFixture, protocol.PlanReviewRef, protocol.ControlItem) {
	t.Helper()
	session := &planReviewFixture{}
	ref := protocol.PlanReviewRef{
		SessionID: domain.SessionID(resumeThreadA), ThreadID: resumeThreadA,
		Sample: domain.AssistantSampleReference{TurnID: "01900000-0000-7000-8000-000000000010"},
	}
	model := NewModel(context.Background(), "test", session).WithTaskProgressScope(ref.SessionID, ref.ThreadID)
	model.browser.thread = ref.ThreadID
	model.workMode.mode = domain.WorkModePlan
	model = updateModel(t, model, planReviewTurnItem(t, "plan-turn", ref, protocol.NewTurnStarted()))
	model = updateModel(t, model, planReviewTurnItem(t, "plan-turn", ref, mustRuntimeCompleted(t)))
	offer, err := protocol.NewPlanReviewOfferItem("plan-turn", ref, markdown)
	if err != nil {
		t.Fatal(err)
	}
	return model, session, ref, offer
}

func planReviewTurnItem(t *testing.T, request protocol.RequestID, ref protocol.PlanReviewRef, event protocol.Event) protocol.ControlItem {
	t.Helper()
	event.SessionID, event.ThreadID, event.TurnID = ref.SessionID, ref.ThreadID, ref.Sample.TurnID
	item, err := protocol.NewCorrelatedTurnItem(request, event)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestPlanReviewPreservesFocusAndHandoffGeneration(t *testing.T) {
	model, session, ref, offer := readyPlanReview(t, "## Plan\n- First\n")
	draft := composerWithText(t, "first draft\nsecond line")
	draft.moveTo(2)
	model.composer = draft
	model = updateModel(t, model, offer)
	if model.planReview.open || model.composer.text != draft.text || model.composer.cursor != 2 || !strings.Contains(model.View(), "/plan review") {
		t.Fatal("offer displaced an editable draft or did not provide the local entry point")
	}
	model.composer = composerWithText(t, "/plan review")
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if !model.planReview.open || model.composer.text != "" || len(model.composer.history.entries) != 0 {
		t.Fatal("manual review did not consume just its local command")
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model = updateModel(t, model, offer)
	if model.planReview.open || model.workMode.mode != domain.WorkModePlan || session.approved.Kind() != "" {
		t.Fatal("the default choice started implementation or the duplicate reopened review")
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("revise the plan")})
	model, submit := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model = updateModel(t, model, testAdmissionMessage(model, submit))
	if model.planReview.offer.Ref.Sample.TurnID.Valid() {
		t.Fatal("accepted revision left the previous review eligible")
	}
	model, _, _, offer = readyPlanReview(t, "## Plan\n- Scope\n")
	model.reading.detached = true
	model = updateModel(t, model, offer)
	if model.planReview.open || model.planReview.offer.Ref != ref {
		t.Fatal("offer displaced detached transcript reading")
	}

	model, _, _, offer = readyPlanReview(t, "## Plan\n- Picker\n")
	shell, _ := resumeTestShell(model)
	generation := shell.generation
	shell = resumeDirect(shell, resumeThreadB)
	shell = pickerUpdate(shell, scopedChatMessage{generation: generation, value: offer})
	shell = pickerUpdate(shell, PickerResult{Request: shell.request, Stage: PickerCancelled})
	if shell.chat.planReview.open || shell.chat.planReview.offer.Ref != ref {
		t.Fatal("picker did not preserve the source candidate without opening it")
	}
	shell.chat.composer = composerWithText(t, "/plan review")
	shell = pickerKey(shell, tea.KeyEnter)
	if !shell.chat.planReview.open {
		t.Fatal("cancelled handoff prevented a manual review")
	}
	shell.mount(resumeCandidate(t, resumeThreadB, "target"), true)
	shell = pickerUpdate(shell, scopedChatMessage{generation: generation, value: offer})
	if shell.chat.planReview.offer.Ref.Sample.TurnID.Valid() || shell.chat.planReview.open {
		t.Fatal("a retired chat supplied a plan to the target")
	}
}

func TestPlanReviewConfirmationOwnsOnlyItsRequest(t *testing.T) {
	markdown := "# Plan\r\n\tEdit @src without invoking /skill.\r\n"
	model, session, ref, offer := readyPlanReview(t, markdown)
	model = updateModel(t, model, offer)
	if got := model.View(); !strings.Contains(got, "Review plan · source\n# Plan\\u{000D}\n\\u{0009}Edit") || !strings.Contains(got, "\n> Keep planning\n  "+planReviewImplement) {
		t.Fatalf("raw review lost safe bytes or the default: %q", got)
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\n/plan off"), Paste: true})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 25, Height: 4})
	model, cmd := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || model.planReview.waiting() || !strings.Contains(model.View(), "resize") {
		t.Fatal("paste or a view without controls started approval")
	}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 65, Height: 12})
	if model.planReview.implement || model.composer.text != "" {
		t.Fatal("resizing back preserved the implementation selection or inserted pasted text")
	}

	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("no typed confirmation command")
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyCtrlC})
	model = updateModel(t, model, cmd())
	if model.planReview.waiting() || !model.planReview.open || model.workMode.mode != domain.WorkModePlan || model.quitRequested || session.approved.Kind() != "" {
		t.Fatal("cancellation before admission affected the session or proposal")
	}

	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyUp})
	model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || cmd() != nil || session.approved.PlanReviewRef() != ref {
		t.Fatal("approval did not submit the displayed sample identity")
	}
	accepted, err := protocol.NewAcceptedInputResult(session.approved, protocol.CommandDispositionStarting, implementationEntry)
	if err != nil {
		t.Fatal(err)
	}
	result, _ := protocol.NewCommandResultItem(accepted)
	model = updateModel(t, model, result)
	if len(model.inputs) != 1 || model.inputs[0].text != domain.PlanImplementationInput(markdown) || len(model.composer.history.entries) != 0 {
		t.Fatal("admission lost exact input or entered recall ahead of Sync")
	}
	shell, _ := resumeTestShell(model)
	shell = pickerUpdate(shell, scopedChatMessage{generation: shell.generation, value: eventStreamClosedMsg{}})
	if shell.closing || shell.chat.planReview.waiting() || shell.chat.planReview.open || shell.chat.workMode.mode != domain.WorkModeDefault {
		t.Fatal("closure ahead of the snapshot left a pending review or discarded the known result")
	}
	request := session.approved.RequestID()
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyCtrlC})
	model = updateModel(t, model, workModeItem(t, request, resumeThreadA, domain.WorkModeDefault))
	model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil || model.quitRequested || model.interruptNext || model.planReview.starting != request || !strings.Contains(model.View(), "Starting plan implementation") {
		t.Fatal("Ctrl+C was routed ahead of the admitted turn's actual start")
	}
	ref.Sample.TurnID = domain.TurnID(implementationEntry)
	other := ref
	other.ThreadID = resumeThreadB
	model = updateModel(t, model, planReviewTurnItem(t, request, other, protocol.NewTurnStarted()))
	if model.planReview.starting != request || model.activeTurnID.Valid() {
		t.Fatal("another chat's start cleared the implementation guard")
	}
	model = updateModel(t, model, planReviewTurnItem(t, request, ref, protocol.NewTurnStarted()))
	input, _ := domain.NewUserInput(implementationEntry, domain.PlanImplementationInput(markdown))
	committed, _ := protocol.NewUserInputCommitted(input)
	model = updateModel(t, model, planReviewTurnItem(t, request, ref, committed))
	model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil || model.planReview.waiting() || len(model.composer.history.entries) != 1 || model.composer.history.entries[0] != normalizeComposerText(input.Text()) || model.transcript[len(model.transcript)-1].text != input.Text() {
		t.Fatal("committed input or active interrupt did not follow the normal path")
	}
	cmd()
	if session.lastInterruptedTurn != ref.Sample.TurnID {
		t.Fatal("normal control did not target the actual implementation turn")
	}
}

func TestPlanReviewViewportKeepsExactSourceReachable(t *testing.T) {
	model, _, _, offer := readyPlanReview(t, "# Heading\n\n"+strings.Repeat("*", 9000)+"\n\nLast visible line\n")
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 42, Height: 9})
	model = updateModel(t, model, offer)
	if got := model.View(); !strings.Contains(got, "Review plan · source") || renderedChatRows(got) > 9 {
		t.Fatalf("budget failure did not use a full raw viewport: %q", got)
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnd})
	if got := model.View(); !strings.Contains(got, "Last visible line") || !strings.Contains(got, "\n> Keep planning") {
		t.Fatalf("raw viewport could not reach the end with fixed controls: %q", got)
	}
	model, _, _, offer = readyPlanReview(t, "# Title\n\nReview `@file`.\n")
	model = updateModel(t, model, offer)
	parses := model.markdown.parser.parses
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyPgDown})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyDown})
	if parses == 0 || model.markdown.parser.parses != parses || !strings.Contains(model.View(), "Review plan · Markdown") {
		t.Fatal("the modal did not reuse its prepared Markdown across scroll and selection")
	}
	raw := newPlanReviewViewport("LF\nCRLF\r\n\t\x1b\\u{001B}").source
	if raw != "LF\nCRLF\\u{000D}\n\\u{0009}\\u{001B}\\\\u{001B}" {
		t.Fatalf("literal escapes and original controls were visually ambiguous: %q", raw)
	}
}
