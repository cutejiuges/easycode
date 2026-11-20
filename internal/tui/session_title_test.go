package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

type sessionTitleFixture struct {
	fakeChatSession
	commands []protocol.Command
}

func (s *sessionTitleFixture) RenameSession(ctx context.Context, id protocol.RequestID, title domain.SessionTitle) (protocol.CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.CommandResult{}, err
	}
	command, err := protocol.NewRenameSessionCommand(id, title)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	s.commands = append(s.commands, command)
	return protocol.NewAcceptedCommandResult(command, protocol.CommandDispositionUpdated)
}

func deliverTitleResult(t *testing.T, model Model, command protocol.Command, rejected protocol.ControlErrorCode) (Model, tea.Cmd) {
	t.Helper()
	var result protocol.CommandResult
	var err error
	if rejected == "" {
		result, err = protocol.NewAcceptedCommandResult(command, protocol.CommandDispositionUpdated)
	} else {
		result, err = protocol.NewRejectedCommandResult(command, rejected)
	}
	if err != nil {
		t.Fatal(err)
	}
	item, err := protocol.NewCommandResultItem(result)
	if err != nil {
		t.Fatal(err)
	}
	updated, cmd := model.projectControlItem(item)
	return updated.(Model), cmd
}

func setTitleCommandDraft(t *testing.T, model Model, text string) Model {
	t.Helper()
	if err := model.composer.replace(0, len(model.composer.text), text); err != nil {
		t.Fatal(err)
	}
	return model
}

func TestSessionTitleInlineUsesLiteralArgumentsAndWaitsForControl(t *testing.T) {
	s := &sessionTitleFixture{}
	f := &fileQueryFixture{results: make(chan domain.FileSearchSnapshot)}
	model := NewModel(context.Background(), "test", s).WithFileQueries(f)
	model = setTitleCommandDraft(t, model, "/rename draft-only\\")
	model, continuation := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if continuation != nil || model.composer.text != "/rename draft-only\n" || model.title.request != nil || len(s.commands) != 0 {
		t.Fatal("composer continuation dispatched the title command")
	}
	model = setTitleCommandDraft(t, model, "/rename\u00a0 Parser 👩‍💻 @src/main.go \\")
	// 光标不在反斜杠之后时，命令参数仍保留原始字符。
	model.composer.moveTo(len(model.composer.text) - 1)
	model, cmd := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || model.title.request == nil || model.title.value != "" || !strings.Contains(model.View(), "Saving session title") {
		t.Fatal("inline rename did not wait for authoritative confirmation")
	}
	if message := cmd(); message != nil || len(s.commands) != 1 {
		t.Fatalf("rename method produced a second UI event or no command: %T, %d commands", message, len(s.commands))
	}
	want := domain.SessionTitle("Parser 👩‍💻 @src/main.go \\")
	if s.commands[0].SessionTitle() != want || len(f.queries) != 0 || len(s.submits) != 0 {
		t.Fatalf("title changed or command triggered file discovery or a prompt: %q", s.commands[0].SessionTitle())
	}
	model, _ = deliverTitleResult(t, model, s.commands[0], "")
	if model.title.value != want || model.title.request != nil || model.composer.text != "" || len(model.composer.history.entries) != 0 {
		t.Fatal("successful rename did not consume the local command exclusively")
	}
	view := model.View()
	if !strings.Contains(view, "Session: "+string(want)) || !strings.Contains(view, "Session renamed to: "+string(want)) {
		t.Fatalf("missing confirmed title and short status: %s", view)
	}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 19, Height: 8})
	for _, line := range strings.Split(strings.TrimSuffix(model.View(), "\n"), "\n") {
		if ansi.StringWidth(line) > 19 || strings.Contains(line, "👩") && !strings.Contains(line, "👩‍💻") {
			t.Fatalf("narrow title exceeded its width or split a grapheme: %q", line)
		}
	}
	model = setTitleCommandDraft(t, model, "next prompt")
	model, _ = deliverTitleResult(t, model, s.commands[0], "")
	if model.composer.text != "next prompt" {
		t.Fatal("late duplicate confirmation consumed a later draft")
	}
}

func TestSessionTitleEditorUsesCompletionAndPreservesFocus(t *testing.T) {
	s := &sessionTitleFixture{}
	model := NewModel(context.Background(), "test", s).WithSessionTitle("现名 👩‍💻")
	model = model.WithSkills([]domain.SkillListEntry{completionSkillEntry(t, "rename", "User skill")})
	model = setTitleCommandDraft(t, model, "/ren")
	model, cmd := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil || model.title.editor != nil || model.composer.text != "/rename " {
		t.Fatal("Tab did not restrict itself to completing the local command")
	}
	model = setTitleCommandDraft(t, model, "/ren")
	model.composer.moveTo(3)
	model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || model.title.editor == nil || model.title.editor.text != "现名 👩‍💻" || model.composer.text != "/ren" || model.composer.cursor != 3 {
		t.Fatal("completion Enter did not open the prefilled editor over the actual draft")
	}
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("new\n\u202einvalid"), Paste: true},
		{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("x", maxSessionTitleEditorBytes)), Paste: true},
		{Type: tea.KeyEnter, Alt: true},
		{Type: tea.KeyCtrlJ},
	} {
		before := model.title.editor
		model, cmd = updateModelWithCommand(t, model, key)
		if cmd != nil || model.title.editor.text != before.text || model.title.editor.cursor != before.cursor {
			t.Fatalf("invalid or multiline key changed editor state or saved: %v", key.Type)
		}
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" @draft\\"), Paste: true})
	if model.title.editor.text != "现名 👩‍💻 @draft\\" || model.title.request != nil {
		t.Fatal("valid paste was not an atomic edit")
	}
	approval := mustApprovalRequested(t)
	model = updateModel(t, model, RuntimeEventMsg{Event: approval})
	edited := model.title.editor.text
	model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || cmd() != nil || len(s.approvals) != 1 || len(s.commands) != 0 || model.title.editor.text != edited {
		t.Fatal("late approval did not preempt the title editor")
	}
	resolved, err := protocol.NewToolApprovalResolved(protocol.ToolApprovalResolvedPayload{
		RequestID: eventApprovalRequestID, InvocationID: eventApprovalInvocationID,
		Fingerprint: strings.Repeat("a", 64), Decision: protocol.ApprovalDeny,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved.TurnID = approval.TurnID
	model = updateModel(t, model, RuntimeEventMsg{Event: resolved})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 27, Height: 7})
	model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil || model.title.editor != nil || model.composer.text != "/ren" || model.composer.cursor != 3 || model.completion.rows != 0 {
		t.Fatal("cancelling after focus changes did not restore the original draft and suppress completion")
	}
	model = setTitleCommandDraft(t, model, "/rename")
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" @draft\\"), Paste: true})
	model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || cmd() != nil || len(s.commands) != 1 || s.commands[0].SessionTitle() != domain.SessionTitle(edited) {
		t.Fatal("editor did not submit the literal edited value once")
	}
	model, _ = deliverTitleResult(t, model, s.commands[0], "")
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("$ren")})
	model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || model.title.editor != nil || model.composer.text != "$rename " {
		t.Fatal("same-name dollar Skill completion did not remain an independent action")
	}
}

func TestSessionTitleRejectsInvalidDraftAndBusySaveWithoutConsumingInput(t *testing.T) {
	s := &sessionTitleFixture{}
	model := NewModel(context.Background(), "test", s)
	for _, text := range []string{"/rename\t", "/rename\nTitle", "/rename Title\n", "/rename Title \u202e", "/rename " + strings.Repeat("复", 86)} {
		model = setTitleCommandDraft(t, model, text)
		next, cmd := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil || next.title.editor != nil || next.composer.text != text || !strings.HasPrefix(next.errorSummary, "invalid_input:") {
			t.Fatalf("invalid local command escaped validation or lost its draft: %q", text)
		}
		model = next
	}
	model.activeTurnID = "01900000-0000-7000-8000-000000000001"
	model = setTitleCommandDraft(t, model, "/rename")
	model, cmd := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || model.title.editor != nil || model.composer.text != "/rename" || !strings.HasPrefix(model.errorSummary, "session_busy:") {
		t.Fatal("known busy operation did not reject the bare editor command")
	}
	model = setTitleCommandDraft(t, model, "/rename After review")
	model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || cmd() != nil || len(s.commands) != 1 {
		t.Fatal("explicit title did not reach the authoritative owner")
	}
	model, _ = deliverTitleResult(t, model, s.commands[0], protocol.ControlErrorSessionBusy)
	if model.title.request != nil || model.title.closed || model.title.value != "" || model.composer.text != "/rename After review" || !strings.HasPrefix(model.errorSummary, "session_busy:") {
		t.Fatal("busy rejection consumed the title draft or shut down the active session")
	}
}

func TestSessionTitleExitAndClosedControlDoNotLoseLastConfirmation(t *testing.T) {
	for _, stage := range []string{"before admission", "after admission", "uncertain", "control closed"} {
		t.Run(stage, func(t *testing.T) {
			s := &sessionTitleFixture{}
			model := NewModel(context.Background(), "test", s).WithSessionTitle("Last confirmed")
			model = setTitleCommandDraft(t, model, "/rename next")
			model, cmd := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
			if cmd == nil || model.title.request == nil {
				t.Fatal("missing pending title write")
			}
			execute := cmd
			if stage != "before admission" && execute() != nil {
				t.Fatal("delivered write emitted an extra UI event")
			}
			if stage == "before admission" || stage == "after admission" {
				model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyCtrlC})
				if cmd != nil || model.title.request == nil || !model.title.request.quit {
					t.Fatal("Ctrl+C abandoned the pending request instead of waiting")
				}
			}
			want := domain.SessionTitle("Last confirmed")
			switch stage {
			case "before admission":
				model, cmd = updateModelWithCommand(t, model, execute())
				if len(s.commands) != 0 {
					t.Fatal("cancellation reached owner as a new title write")
				}
			case "after admission":
				model, cmd = deliverTitleResult(t, model, s.commands[0], "")
				want = "next"
			case "uncertain":
				model, _ = deliverTitleResult(t, model, s.commands[0], protocol.ControlErrorSessionFailed)
			case "control closed":
				model, _ = updateModelWithCommand(t, model, eventStreamClosedMsg{})
			}
			if model.title.request != nil || model.title.value != want {
				t.Fatal("terminal result failed to preserve the confirmed title and clear the request")
			}
			if stage == "uncertain" || stage == "control closed" {
				model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
				if !model.title.closed || cmd != nil || model.composer.text != "/rename next" || !strings.Contains(model.View(), "Error:") {
					t.Fatal("failed control left the session writable or did not report a safe error")
				}
				model, cmd = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyCtrlC})
			}
			if cmd == nil {
				t.Fatal("resolved request or failed session blocked exit with a nonempty draft")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatal("resolved exit did not request normal TUI shutdown")
			}
		})
	}
}

func TestMountedTitleStreamClosureKeepsTheLastConfirmation(t *testing.T) {
	session := &sessionTitleFixture{}
	chat := NewModel(context.Background(), "test", session).WithSessionTitle("Last confirmed")
	model, host := resumeTestShell(chat)
	model = pickerKey(resumeType(model, "/rename next"), tea.KeyEnter)
	if model.chat.title.request == nil {
		t.Fatal("title command did not start")
	}
	model = pickerUpdate(model, scopedChatMessage{generation: model.generation, value: eventStreamClosedMsg{}})
	if host.stopped || !model.chat.title.closed || model.chat.title.request != nil ||
		!strings.Contains(model.View(), "Last confirmed") || !strings.Contains(model.View(), "could not confirm") {
		t.Fatalf("mounted chat lost the last confirmed title or terminal save failure: %s", model.View())
	}
	model = pickerKey(model, tea.KeyEnter)
	if len(host.requests) != 0 || model.chat.title.request != nil {
		t.Fatal("closed chat accepted another request")
	}
	model = pickerKey(model, tea.KeyCtrlC)
	if !host.stopped || model.ExitCode() != 0 {
		t.Fatal("closed chat did not request normal host shutdown")
	}
}
