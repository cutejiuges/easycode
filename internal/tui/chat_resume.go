package tui

import (
	"strings"

	"easycode/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	resumeCommandName = "resume"
	branchCommandName = "branch"
	forkCommandName   = "fork"
	rewindCommandName = "rewind"
)

type chatHandoffIntent struct {
	operation PickerOperation
	target    domain.ThreadID
	title     domain.SessionTitle
	draft     composer
}

func isBranchCommandName(name string) bool {
	return name == branchCommandName || name == forkCommandName
}

func isBranchSlash(head string) bool {
	return strings.HasPrefix(head, "/") && isBranchCommandName(head[1:])
}

func isHandoffSlash(head string) bool {
	return strings.HasPrefix(head, "/") && isHandoffCommandName(head[1:])
}

func isHandoffCommandName(name string) bool {
	return name == resumeCommandName || name == rewindCommandName || isBranchCommandName(name)
}

func (model Model) submitHandoff(head, argument string) (tea.Model, tea.Cmd) {
	model.completion.suppress(completionCancelled)
	model.suppressFiles(completionCancelled)
	intent := chatHandoffIntent{operation: PickerOpen, draft: model.composer}
	switch {
	case head == "/"+rewindCommandName:
		if strings.TrimSpace(argument) != "" {
			model.errorSummary = "invalid_command: /rewind does not accept arguments"
			return model, nil
		}
		intent.operation = PickerRewind
	case isBranchSlash(head):
		intent.operation = PickerBranch
		title, err := branchTitleArgument(argument)
		if err != nil {
			model.errorSummary = "invalid_input: " + err.Error()
			return model, nil
		}
		intent.title = title
	default:
		argument = strings.TrimSpace(argument)
		if argument == "" {
			break
		}
		target, err := domain.ParseThreadID(argument)
		if err != nil {
			model.errorSummary = "invalid_command: use /resume or /resume <full thread ID>"
			return model, nil
		}
		if target == model.browser.thread {
			model.errorSummary = "This conversation is already open"
			return model, nil
		}
		intent.target = target
	}
	if model.handoffBusy() {
		model.errorSummary = "session_busy: finish the current work or handle pending inputs, then retry"
		return model, nil
	}
	model.handoffIntent = &intent
	return model, nil
}

func (model Model) handoffBusy() bool {
	return model.activeTurnID.Valid() || model.activeCompactionID.Valid() || model.pending != nil ||
		model.retracting != nil || model.workMode.pending != nil || len(model.inputs) != 0 || model.cancelling
}

func branchTitleArgument(argument string) (domain.SessionTitle, error) {
	if !hasSessionTitleArgument(argument) {
		return "", nil
	}
	return domain.ParseSessionTitle(argument)
}

func (model *Model) returnFromHandoff(draft composer, notice string, directoryMismatch bool) {
	model.restoreComposer(draft)
	model.completion.refreshTarget(model.composer)
	model.completion.suppress(completionCancelled)
	model.errorSummary = pickerSafe(notice, 1024)
	if directoryMismatch {
		model.handoffNotice, model.handoffNoticeScroll = model.errorSummary, 0
		model.errorSummary = ""
	}
	model.prepareCompletion()
	model.prepareMarkdownView()
}

func (model Model) handoffNoticeText() string {
	return model.handoffNotice + "\nEsc: back · ↑↓ scroll"
}

func (model *Model) handleHandoffNoticeKey(key tea.KeyMsg) {
	if key.Paste {
		return
	}
	switch key.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		model.handoffNotice = ""
		model.handoffNoticeScroll = 0
		return
	case tea.KeyUp:
		model.handoffNoticeScroll--
	case tea.KeyDown:
		model.handoffNoticeScroll++
	case tea.KeyPgUp:
		model.handoffNoticeScroll -= model.heightOrDefault()
	case tea.KeyPgDown:
		model.handoffNoticeScroll += model.heightOrDefault()
	default:
		return
	}
	rows := renderedChatRows(ansi.Hardwrap(model.handoffNoticeText(), model.widthOrDefault(), true))
	model.handoffNoticeScroll = min(max(0, model.handoffNoticeScroll), max(0, rows-model.heightOrDefault()))
}

func (model Model) handoffStatus() string {
	if model.branchNotice != "" {
		wrapped := ansi.Hardwrap(model.branchNotice, model.widthOrDefault(), true)
		if renderedChatRows(wrapped) > max(1, model.heightOrDefault()-model.composerChatRows()-4) {
			return "Ctrl+G: branch details\n"
		}
		return wrapped + "\n"
	}
	if !model.resumed {
		return ""
	}
	status := "Resumed session " + string(model.browser.thread)
	if len(model.main.items) == 0 && len(model.transcript) == 0 {
		status += "\nNo visible history"
	}
	return ansi.Hardwrap(status, model.widthOrDefault(), true) + "\n"
}
