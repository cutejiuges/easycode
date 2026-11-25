package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type rewindTestHost struct {
	resumeTestHost
	rejectBranch bool
}

func (host *rewindTestHost) Submit(request PickerRequest) bool {
	if host.rejectBranch && request.Operation == PickerBranch {
		return false
	}
	return host.resumeTestHost.Submit(request)
}

func TestRewindUsesLocalCompletionAndKeepsExactSelection(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	files := &fileQueryFixture{}
	source := resumeCandidate(t, resumeThreadA, "source").WithFileQueries(files).WithSkills([]domain.SkillListEntry{
		completionSkillEntry(t, "rewind", "A separate skill"),
	})
	host := &rewindTestHost{rejectBranch: true}
	m := NewChatShell(host, PickerConfig{}, source)
	m = pickerKey(resumeType(m, "$rew"), tea.KeyTab)
	if m.chat.composer.text != "$rewind " || m.chat.skillInvocationText("rewind") != "$rewind " {
		t.Fatal("rewind occupied the explicit Skill entry")
	}
	m.chat.composer.clear()
	m = pickerKey(resumeType(m, "/rew"), tea.KeyTab)
	if m.chat.composer.text != "/rewind " || len(host.requests) != 0 {
		t.Fatal("Tab opened the picker")
	}
	m = pickerKey(resumeType(m, "@src/ma"), tea.KeyEnter)
	if len(files.queries) != 0 || len(host.requests) != 0 || m.chat.composer.text != "/rewind @src/ma" ||
		!strings.Contains(m.chat.errorSummary, "does not accept arguments") {
		t.Fatal("invalid rewind input left the local command path")
	}
	m.chat.composer.clear()
	m = pickerKey(resumeType(m, "/rewind \\"), tea.KeyEnter)
	if !strings.HasSuffix(m.chat.composer.text, "\n") || len(host.requests) != 0 {
		t.Fatal("the ordinary continuation key opened the picker")
	}
	m.chat.composer.clear()
	m = pickerKey(resumeType(m, "/rew"), tea.KeyEnter)
	browse := m.request
	if browse.Operation != PickerRewind || browse.Source != resumeThreadA || len(host.requests) != 1 || m.phase != shellOpening {
		t.Fatalf("expected one local rewind browse, got %#v", host.requests)
	}
	entries := []RewindEntry{
		{RewindSelection: RewindSelection{TurnID: domain.TurnID(resumeThreadA), InputID: domain.InputID(resumeThreadA)}, Number: 2, Summary: "same \x1b[2J"},
		{RewindSelection: RewindSelection{TurnID: domain.TurnID(resumeThreadB), InputID: domain.InputID(resumeThreadB)}, Number: 4, Summary: "same \x1b[2J", HasMultipleInputs: true},
	}
	m = pickerUpdate(m, PickerResult{Request: browse, RewindEntries: entries, HasMore: true})
	view := m.View()
	if !strings.Contains(view, "> Current") || !strings.Contains(view, "Turn 2") || !strings.Contains(view, "Turn 4") ||
		!strings.Contains(view, "latest 200") || strings.Contains(view, "\x1b") {
		t.Fatalf("incorrect default, bound, identity or terminal output:\n%s", view)
	}
	m = pickerKey(pickerKey(m, tea.KeyUp), tea.KeyEnter)
	m = pickerUpdate(m, tea.KeyMsg{Type: tea.KeyEnter, Paste: true, Runes: []rune("\n")})
	m = pickerUpdate(m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = pickerKey(m, tea.KeyCtrlJ)
	if view = m.View(); !strings.Contains(view, "Turn 4") || !strings.Contains(view, "first initial input") ||
		!strings.Contains(view, "> Back") || len(host.requests) != 1 {
		t.Fatalf("confirmation did not preserve the selected turn and default:\n%s", view)
	}
	m = pickerKey(m, tea.KeyDown)
	m = pickerUpdate(m, tea.WindowSizeMsg{Width: 12, Height: 5})
	m = pickerKey(m, tea.KeyDown)
	m = pickerUpdate(m, tea.WindowSizeMsg{Width: 62, Height: 20})
	if view = m.View(); !strings.Contains(view, "Turn 4") || !strings.Contains(view, "> Back") {
		t.Fatalf("resize changed selection or preserved a hidden confirmation:\n%s", view)
	}
	m = pickerKey(m, tea.KeyEsc)
	if !strings.Contains(m.View(), "> Turn 4") {
		t.Fatal("returning from confirmation lost the selected turn")
	}
	m = pickerKey(pickerKey(pickerKey(m, tea.KeyEnter), tea.KeyDown), tea.KeyEnter)
	if len(host.cancelRequests) != 1 || host.cancelRequests[0] != browse || m.phase != shellCancelling {
		t.Fatal("rejected confirmation did not release the accepted browse")
	}
	m = pickerUpdate(m, PickerResult{Request: browse, Stage: PickerCancelled})
	if m.phase != shellChat || m.chat.composer.text != "/rew" || m.chat.composer.cursor != len("/rew") ||
		m.chat.completion.rows != 0 || len(source.session.(*fakeChatSession).submits) != 0 {
		t.Fatal("cancel did not restore the pre-completion source draft")
	}
}

type rewindInputSession struct {
	fakeChatSession
	last domain.InputDraft
}

func (s *rewindInputSession) Append(ctx context.Context, draft domain.InputDraft) (protocol.CommandResult, error) {
	s.last = draft
	return s.fakeChatSession.Append(ctx, draft)
}

func TestRewindActivationGuardsUneditedDraftFromBufferedEnter(t *testing.T) {
	m, host := resumeTestShell(resumeCandidate(t, resumeThreadA, "source"))
	m = pickerKey(resumeType(m, "/rewind"), tea.KeyEnter)
	browse := m.request
	selection := RewindSelection{TurnID: domain.TurnID(resumeThreadA), InputID: domain.InputID(resumeThreadA)}
	m = pickerUpdate(m, PickerResult{Request: browse, RewindEntries: []RewindEntry{{RewindSelection: selection, Number: 3, Summary: "/rewind"}}})
	for _, key := range []tea.KeyType{tea.KeyUp, tea.KeyEnter, tea.KeyDown, tea.KeyEnter, tea.KeyEnter} {
		m = pickerKey(m, key)
	}
	confirmation := m.request
	if len(host.requests) != 2 || confirmation.Operation != PickerBranch || confirmation.BrowseID != browse.ID ||
		confirmation.ID == browse.ID || confirmation.RewindSelection != selection {
		t.Fatalf("duplicate or mismatched branch confirmation: %#v", host.requests)
	}
	s := &rewindInputSession{}
	target := resumeCandidate(t, resumeThreadB, "").WithSessionTitle("Past (Branch)")
	target.session, target.outputs = s, s.Outputs()
	result := PickerResult{Request: confirmation, Stage: PickerPrepared, Chat: &target, BranchThreadID: resumeThreadB, BranchPublication: BranchPublished}
	m = pickerUpdate(m, result)
	m = pickerKey(m, tea.KeyEnter)
	if m.phase != shellRetiring || m.chat.browser.thread != resumeThreadA || len(host.accepted) != 1 || len(s.submits) != 0 {
		t.Fatal("branch entered chat before activation or accepted a buffered key")
	}
	result.Stage = PickerActivated
	result.RewindDraft = RewindDraft{Text: "/rewind\r\n @src/main.go"}
	m = pickerUpdate(m, result)
	if m.phase != shellChat || m.chat.composer.text != "/rewind\n @src/main.go" || m.chat.files.rows != 0 || m.chat.completion.rows != 0 ||
		!strings.Contains(m.View(), "Past (Branch)") || !strings.Contains(strings.ReplaceAll(m.View(), "\n", ""), "/resume "+string(resumeThreadA)) {
		t.Fatalf("activation lost the recalled draft or source recovery hint:\n%s", m.View())
	}
	m = pickerKey(m, tea.KeyLeft)
	m = pickerUpdate(m, tea.WindowSizeMsg{Width: 72, Height: 20})
	for range 3 {
		m = pickerKey(m, tea.KeyEnter)
	}
	if m.chat.pending != nil || len(host.requests) != 2 || !strings.Contains(m.View(), "> Back") || !strings.Contains(m.View(), rewindSendLabel) {
		t.Fatal("post-activation Enter sent the unedited draft")
	}
	next, appendInput := m.chat.Update(tea.KeyMsg{Type: tea.KeyDown})
	chat := next.(Model)
	if appendInput != nil {
		t.Fatal("selecting the send action submitted without confirmation")
	}
	chat, appendInput = updateModelWithCommand(t, chat, tea.KeyMsg{Type: tea.KeyEnter})
	_ = testAdmissionMessage(chat, appendInput)
	if len(s.submits) != 1 || s.last.Text() != "/rewind\n @src/main.go" || s.last.SkillIntent() != domain.InputSkillLiteral ||
		len(s.last.FileReferences()) != 1 || chat.handoffIntent != nil {
		t.Fatal("historical literal text did not follow the ordinary input preparation boundary")
	}
}

func TestRewindEditedDraftRetainsOrReleasesFirstWordIntent(t *testing.T) {
	s := &rewindInputSession{fakeChatSession: fakeChatSession{submitErr: errors.New("skill source unavailable")}}
	m := NewModel(context.Background(), "test", s)
	m.handoffEnabled = true
	if err := m.installRewindDraft(RewindDraft{Text: "/rewind arg", HasSkill: true}); err != nil {
		t.Fatal(err)
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m, submit := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = updateModel(t, m, testAdmissionMessage(m, submit))
	if s.last.SkillIntent() != domain.InputSkillRequired || m.composer.text != "/rewind args" ||
		m.composer.rewind.intent != domain.InputSkillRequired || m.pending != nil {
		t.Fatal("edited Skill arguments or preparation failure dropped the original Skill intent")
	}
	m.composer.moveTo(len("/rewind"))
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ed")})
	m, submit = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	_ = testAdmissionMessage(m, submit)
	if s.last.SkillIntent() != domain.InputSkillDefault || s.last.Text() != "/rewinded args" {
		t.Fatal("changing the first word did not restore normal dispatch")
	}
}

func TestRewindReturnedInputRetainsFirstWordIntent(t *testing.T) {
	s := &rewindInputSession{}
	m := NewModel(context.Background(), "test", s)
	const text = "/compact @src/x"
	if err := m.installRewindDraft(RewindDraft{Text: text}); err != nil {
		t.Fatal(err)
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m, submit := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = updateModel(t, m, testAdmissionMessage(m, submit))
	if len(m.inputs) != 1 || s.last.SkillIntent() != domain.InputSkillLiteral {
		t.Fatal("historical literal prompt was not admitted as conversation input")
	}
	status, err := protocol.NewInputStatus("ui-input", m.inputs[0].id, protocol.InputReturned, "", protocol.InputReasonPreflight)
	if err != nil {
		t.Fatal(err)
	}
	item, err := protocol.NewInputStatusItem(status)
	if err != nil {
		t.Fatal(err)
	}
	m = updateModel(t, m, item)
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("follow-up notes")})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	const retried = text + "\nfollow-up notes"
	if m.composer.text != retried || m.composer.rewind.intent != domain.InputSkillLiteral || m.composer.rewind.unedited {
		t.Fatalf("returned historical input lost its literal draft: %q (intent %v)", m.composer.text, m.composer.rewind.intent)
	}

	m, submit = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	_ = testAdmissionMessage(m, submit)
	if len(s.submits) != 2 || s.last.Text() != retried || s.last.SkillIntent() != domain.InputSkillLiteral || len(s.last.FileReferences()) != 0 {
		t.Fatal("retry did not send the historical literal as conversation input")
	}
}

func TestRewindRetractionKeepsTheFirstQueuedInputIntent(t *testing.T) {
	s := &rewindInputSession{}
	m := NewModel(context.Background(), "test", s)
	m.handoffEnabled = true
	if err := m.installRewindDraft(RewindDraft{Text: "/rewind arg", HasSkill: true}); err != nil {
		t.Fatal(err)
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m, submit := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = updateModel(t, m, testAdmissionMessage(m, submit))
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("trailing draft")})
	m, retract := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = updateModel(t, m, testAdmissionMessage(m, retract))
	m = updateModel(t, m, <-s.Outputs())
	const retried = "/rewind args\ntrailing draft"
	if m.composer.text != retried || m.composer.rewind.intent != domain.InputSkillRequired || len(m.inputs) != 0 {
		t.Fatal("retraction lost the first queued input's Skill intent")
	}
	m, submit = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	_ = testAdmissionMessage(m, submit)
	if len(s.submits) != 2 || s.last.Text() != retried || s.last.SkillIntent() != domain.InputSkillRequired || m.handoffIntent != nil {
		t.Fatal("retry did not request the Skill anew")
	}
}
