package tui

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	resumeThreadA domain.ThreadID = "01900000-0000-7000-8000-000000000001"
	resumeThreadB domain.ThreadID = "01900000-0000-7000-8000-000000000002"
)

type resumeTestHost struct {
	pickerTestHost
	cancelRequests []PickerRequest
	accepted       []uint64
}

type compactingResumeSession struct {
	sessionTitleFixture
	contextSession
	cancelled domain.CompactionID
}

func (s *compactingResumeSession) CancelCompaction(_ context.Context, id domain.CompactionID) (protocol.CommandResult, error) {
	s.cancelled = id
	command, err := protocol.NewCancelCompactionCommand("test-cancel", id)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	return protocol.NewAcceptedCommandResult(command, protocol.CommandDispositionCancelling)
}

func (h *resumeTestHost) Accept(id uint64) bool {
	h.accepted = append(h.accepted, id)
	return true
}

func (h *resumeTestHost) CancelResume(request PickerRequest) {
	h.cancelRequests = append(h.cancelRequests, request)
}

func resumeChatFixture(thread domain.ThreadID, page protocol.TranscriptListPage) Model {
	return NewModel(context.Background(), "test", &fakeChatSession{}).
		WithTaskProgressScope(domain.SessionID(thread), thread).
		WithTranscriptQueries(thread, &mainTestQueries{page: page})
}

func resumeCandidate(t *testing.T, thread domain.ThreadID, text string) Model {
	t.Helper()
	var page protocol.TranscriptListPage
	if text != "" {
		page.Items = []protocol.TranscriptListItem{{
			Cursor: protocol.TranscriptCursor{Sequence: 1}, Kind: domain.TranscriptAssistant, TurnID: domain.TurnID(thread), Text: text,
		}}
	}
	model, err := resumeChatFixture(thread, page).LoadInitialTranscript(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func resumeTestShell(chat Model) (StartupModel, *resumeTestHost) {
	host := &resumeTestHost{}
	return NewChatShell(host, PickerConfig{Family: domain.ProviderOpenAI, Wire: "responses", Model: "gpt", CWD: "/here"}, chat), host
}

func resumeType(model StartupModel, text string) StartupModel {
	return pickerUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

func resumeDirect(model StartupModel, thread domain.ThreadID) StartupModel {
	return pickerKey(resumeType(model, "/resume "+string(thread)), tea.KeyEnter)
}

func TestInChatResumePreservesCompletedDraftAndPortOnFailure(t *testing.T) {
	model, host := resumeTestShell(resumeChatFixture(resumeThreadA, protocol.TranscriptListPage{}))
	model = resumeType(model, "/res")
	model = pickerKey(model, tea.KeyEnter)
	if len(host.requests) != 1 || model.phase != shellOpening || model.request.Source != resumeThreadA || model.request.Operation != PickerOpen {
		t.Fatal("Enter did not submit exactly one local chooser request")
	}
	model = pickerUpdate(model, tea.WindowSizeMsg{Width: 53, Height: 19})
	model = pickerKey(model, tea.KeyEsc)
	if len(host.cancelRequests) != 1 || model.phase != shellCancelling {
		t.Fatal("cancel returned before the host finished")
	}
	model = pickerUpdate(model, PickerResult{Request: model.request, Stage: PickerCancelled, Error: "Resume cancelled"})
	if model.phase != shellChat || model.chat.composer.text != "/res" || model.chat.composer.cursor != 4 || model.chat.composer.width != 53 || model.chat.completion.rows != 0 {
		t.Fatal("cancel did not restore the original pre-completion draft")
	}
	if !strings.Contains(model.View(), "Resume cancelled") || len(model.chat.session.(*fakeChatSession).submits) != 0 {
		t.Fatal("local request reached chat or lost the cancellation notice")
	}
	model.chat.composer.clear()
	model = resumeDirect(model, resumeThreadB)
	request := model.request
	model = pickerUpdate(model, PickerResult{Request: request, Stage: PickerCancelled, Error: "session_busy: target is active"})
	if model.chat.composer.text != "/resume "+string(resumeThreadB) || !strings.Contains(model.View(), "target is active") || host.stopped {
		t.Fatal("failed formal recovery changed the source or lost the draft")
	}
	model = pickerUpdate(model, PickerResult{Request: request, Stage: PickerPrepared})
	if model.phase != shellChat || len(host.accepted) != 0 {
		t.Fatal("late result reopened a failed switch")
	}
}

func TestInChatCommandsKeepPreCompactCancellationInChat(t *testing.T) {
	session := &compactingResumeSession{}
	chat := NewModel(context.Background(), "test", session).WithTranscriptQueries(resumeThreadA, &mainTestQueries{})
	id := domain.CompactionID("01900000-0000-7000-8000-000000000023")
	chat.prepareCompaction(id, domain.ManualCompactionScope())
	chat.compaction.stage = compactionChecking
	model, host := resumeTestShell(chat)
	for _, draft := range []string{"/resume " + string(resumeThreadB), "/rename New title"} {
		model.chat.composer.clear()
		model = resumeType(model, draft)
		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(StartupModel)
		if cmd != nil || model.phase != shellChat || len(host.requests) != 0 || model.chat.title.request != nil ||
			model.chat.composer.text != draft || !strings.HasPrefix(model.chat.errorSummary, "session_busy:") {
			t.Fatalf("%q took over an active compaction or lost its draft", draft)
		}
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = updated.(StartupModel)
	if cmd == nil || !model.chat.compaction.cancelling {
		t.Fatal("Ctrl+C did not reach the compaction owner")
	}
	cmd()
	if session.cancelled != id || len(host.cancelRequests) != 0 {
		t.Fatal("Ctrl+C did not cancel the current compaction exclusively")
	}
}

func TestInChatResumeHandoffScopesNestedMessagesAndKeepsInputDisabled(t *testing.T) {
	model, host := resumeTestShell(resumeCandidate(t, resumeThreadA, "old A"))
	oldGeneration := model.generation
	oldClose := scopeChatCommand(oldGeneration, func() tea.Msg { return eventStreamClosedMsg{} })()
	oldBatch := scopeChatCommand(oldGeneration, tea.Batch(
		tea.Batch(func() tea.Msg { return mainTranscriptReply{generation: model.chat.main.generation} }, tea.Quit), tea.Quit,
	))()
	_, commands := model.Update(oldBatch)
	nested := commands().(tea.BatchMsg)[0]()
	model = resumeDirect(model, resumeThreadB)
	b := resumeCandidate(t, resumeThreadB, "").WithSessionTitle("Named B")
	request := model.request
	model = pickerUpdate(model, PickerResult{Request: request, Stage: PickerPrepared, Chat: &b})
	model = pickerKey(model, tea.KeyEsc)
	model = pickerUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("old paste"), Paste: true})
	model = pickerKey(model, tea.KeyEnter)
	model = pickerUpdate(model, oldClose)
	if model.phase != shellRetiring || len(host.requests) != 1 || len(host.accepted) != 1 || host.stopped || !strings.Contains(model.View(), "closing previous session") {
		t.Fatal("input or early closure changed the accepted handoff")
	}
	model = pickerUpdate(model, PickerResult{Request: request, Stage: PickerActivated, Chat: &b})
	if !strings.Contains(model.View(), string(resumeThreadB)) || !strings.Contains(model.View(), "Session: Named B") ||
		!strings.Contains(model.View(), "No visible history") || strings.Contains(model.View(), "old paste") {
		t.Fatalf("activation failed to render empty target before input: %s", model.View())
	}
	model = resumeDirect(model, resumeThreadA)
	a := resumeCandidate(t, resumeThreadA, "new A").WithSessionTitle("Named A")
	request = model.request
	model = pickerUpdate(model, PickerResult{Request: request, Stage: PickerPrepared, Chat: &a})
	model = pickerUpdate(model, PickerResult{Request: request, Stage: PickerActivated, Chat: &a})
	model = pickerUpdate(model, oldClose)
	model = pickerUpdate(model, nested)
	if host.stopped || !strings.Contains(model.View(), "new A") || !strings.Contains(model.View(), "Session: Named A") ||
		strings.Contains(model.View(), "old A") || strings.Contains(model.View(), "Named B") || model.chat.composer.text != "" {
		t.Fatalf("an earlier activation affected the new instance of A: %s", model.View())
	}
}

func TestInChatChooserKeepsEmptyScopeAndShowsScrollableDirectoryHandoff(t *testing.T) {
	model, host := resumeTestShell(resumeChatFixture(resumeThreadA, protocol.TranscriptListPage{}))
	model = pickerKey(resumeType(model, "/resume"), tea.KeyEnter)
	model = pickerUpdate(model, PickerResult{Request: model.request, Entries: []SessionEntry{{ThreadID: resumeThreadA}}})
	if !strings.Contains(model.View(), "No other sessions") || model.picker.selected != "" {
		t.Fatal("empty scope showed the active conversation")
	}
	model = pickerKey(model, tea.KeyCtrlA)
	entry := SessionEntry{ThreadID: resumeThreadB, Title: "Review payment", CWD: "/different/project", Family: domain.ProviderAnthropic, Wire: "messages", Model: "claude"}
	model = pickerUpdate(model, PickerResult{Request: model.request, Entries: []SessionEntry{entry}})
	model = resumeType(model, "REVIEW")
	model = pickerKey(model, tea.KeySpace)
	model = resumeType(model, "PAYMENT")
	model = pickerKey(model, tea.KeyCtrlV)
	count := len(host.requests)
	model = pickerUpdate(model, PickerResult{Request: model.request, Error: "Session preview is unavailable"})
	model = pickerKey(model, tea.KeyEnter)
	model = pickerKey(model, tea.KeyEsc)
	if model.picker.selected != resumeThreadB || model.picker.query != "REVIEW PAYMENT" || len(host.requests) != count {
		t.Fatal("preview error or keys lost selection or opened a recovery")
	}
	model = pickerUpdate(model, tea.WindowSizeMsg{Width: 24, Height: 4})
	model = pickerKey(model, tea.KeyEnter)
	var lines strings.Builder
	for range 32 {
		lines.WriteString(strings.Split(model.View(), "\n")[0])
		model = pickerKey(model, tea.KeyDown)
	}
	lines.WriteString(strings.ReplaceAll(model.View(), "\n", ""))
	if !strings.Contains(lines.String(), "easycode --resume "+string(resumeThreadB)) ||
		!strings.Contains(lines.String(), "/different/project") || !strings.Contains(lines.String(), "Incompatible") || len(host.requests) != count {
		t.Fatalf("cross-directory hint is incomplete or launched a recovery: %s", lines.String())
	}
}

func TestInChatDirectDirectoryHintKeepsTheCompleteCommand(t *testing.T) {
	model, host := resumeTestShell(resumeChatFixture(resumeThreadA, protocol.TranscriptListPage{}))
	model = pickerUpdate(model, tea.WindowSizeMsg{Width: 30, Height: 4})
	model = resumeDirect(model, resumeThreadB)
	command := "easycode --resume " + string(resumeThreadB)
	model = pickerUpdate(model, PickerResult{Request: model.request, Stage: PickerCancelled, DirectoryMismatch: true,
		Error: "From the session's creation directory, run " + command + " with compatible configuration",
	})
	var lines strings.Builder
	for range 10 {
		view := model.View()
		next := pickerKey(model, tea.KeyDown)
		if next.View() == view {
			lines.WriteString(strings.ReplaceAll(view, "\n", ""))
			break
		}
		lines.WriteString(strings.Split(view, "\n")[0])
		model = next
	}
	if !strings.Contains(lines.String(), command) || model.phase != shellChat || model.chat.browser.thread != resumeThreadA {
		t.Fatalf("the active source lost the full directory handoff hint: %s", lines.String())
	}
	model = pickerKey(model, tea.KeyEnter)
	if len(host.requests) != 1 || len(model.chat.session.(*fakeChatSession).submits) != 0 {
		t.Fatal("notice keys went to the conversation")
	}
	model = pickerKey(model, tea.KeyEsc)
	draft := "/resume " + string(resumeThreadB)
	if model.chat.resumeDirectoryNotice != "" || model.chat.composer.text != draft || model.chat.composer.cursor != len(draft) {
		t.Fatal("dismissal did not expose the restored draft")
	}
}

func TestResumeUsesNormalSubmitAndKeepsFileArgumentsLocal(t *testing.T) {
	source := resumeChatFixture(resumeThreadA, protocol.TranscriptListPage{})
	files := &fileQueryFixture{results: make(chan domain.FileSearchSnapshot)}
	source = source.WithFileQueries(files)
	model, host := resumeTestShell(source)
	model = pickerUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/resume @src/ma"), Paste: true})
	if len(host.requests) != 0 || len(files.queries) != 0 || model.chat.composer.text != "/resume @src/ma" {
		t.Fatal("paste opened a picker, file search or lost text")
	}
	model = pickerKey(model, tea.KeyEnter)
	if len(host.requests) != 0 || len(files.queries) != 0 || !strings.Contains(model.View(), "invalid_command") {
		t.Fatal("malformed resume argument escaped local validation")
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter, Alt: true}, {Type: tea.KeyCtrlJ}, {Type: tea.KeyEnter}} {
		model.chat.composer.clear()
		text := "/resume"
		if !key.Alt && key.Type == tea.KeyEnter {
			text += " \\"
		}
		model = resumeType(model, text)
		model = pickerUpdate(model, key)
		if !strings.HasSuffix(model.chat.composer.text, "\n") || len(host.requests) != 0 {
			t.Fatalf("multiline editing triggered resume: %q", model.chat.composer.text)
		}
	}
	model.chat.composer.clear()
	model = resumeType(model, "$resume @src/ma")
	if len(files.queries) != 1 || len(host.requests) != 0 {
		t.Fatal("resume-named skill lost normal file completion")
	}
}

func TestLoadInitialTranscriptRejectsInvalidPageBeforeAdoption(t *testing.T) {
	chat := resumeChatFixture(resumeThreadB, protocol.TranscriptListPage{
		NextBefore: protocol.TranscriptCursor{Sequence: 3},
	})
	if _, err := chat.LoadInitialTranscript(context.Background()); err == nil {
		t.Fatal("invalid page became an adoptable conversation")
	}
	chat = resumeChatFixture(resumeThreadB, protocol.TranscriptListPage{})
	chat = chat.WithTaskProgressScope(domain.SessionID(resumeThreadA), resumeThreadA)
	if _, err := chat.LoadInitialTranscript(context.Background()); err == nil {
		t.Fatal("another conversation's identity became adoptable")
	}
}
