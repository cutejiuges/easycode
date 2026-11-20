package tui

import (
	"easycode/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const resumeCommandName = "resume"

type chatResumeIntent struct {
	target domain.ThreadID
	draft  composer
}

func (model Model) submitResume(argument string) (tea.Model, tea.Cmd) {
	model.completion.suppress(completionCancelled)
	model.suppressFiles(completionCancelled)
	var target domain.ThreadID
	if argument != "" {
		var err error
		target, err = domain.ParseThreadID(argument)
		if err != nil {
			model.errorSummary = "invalid_command: use /resume or /resume <full thread ID>"
			return model, nil
		}
		if target == model.browser.thread {
			model.errorSummary = "This conversation is already open"
			return model, nil
		}
	}
	if model.activeTurnID.Valid() || model.activeCompactionID.Valid() || model.pending != nil ||
		model.retracting != nil || len(model.inputs) != 0 || model.cancelling {
		model.errorSummary = "session_busy: finish the current work or handle pending inputs, then retry"
		return model, nil
	}
	model.resumeIntent = &chatResumeIntent{target: target, draft: model.composer}
	return model, nil
}

func (model *Model) returnFromResume(draft composer, notice string, directoryMismatch bool) {
	model.restoreComposer(draft)
	model.completion.refreshTarget(model.composer)
	model.completion.suppress(completionCancelled)
	model.errorSummary = pickerSafe(notice, 1024)
	if directoryMismatch {
		model.resumeDirectoryNotice, model.resumeDirectoryScroll = model.errorSummary, 0
		model.errorSummary = ""
	}
	model.prepareCompletion()
	model.prepareMarkdownView()
}

func (model Model) resumeDirectoryText() string {
	return model.resumeDirectoryNotice + "\nEsc: back · ↑↓ scroll"
}

func (model *Model) handleResumeDirectoryKey(key tea.KeyMsg) {
	if key.Paste {
		return
	}
	switch key.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		model.resumeDirectoryNotice = ""
		model.resumeDirectoryScroll = 0
		return
	case tea.KeyUp:
		model.resumeDirectoryScroll--
	case tea.KeyDown:
		model.resumeDirectoryScroll++
	case tea.KeyPgUp:
		model.resumeDirectoryScroll -= model.heightOrDefault()
	case tea.KeyPgDown:
		model.resumeDirectoryScroll += model.heightOrDefault()
	default:
		return
	}
	rows := renderedChatRows(ansi.Hardwrap(model.resumeDirectoryText(), model.widthOrDefault(), true))
	model.resumeDirectoryScroll = min(max(0, model.resumeDirectoryScroll), max(0, rows-model.heightOrDefault()))
}

func (model Model) resumeStatus() string {
	if !model.resumed {
		return ""
	}
	status := "Resumed session " + string(model.browser.thread)
	if len(model.main.items) == 0 && len(model.transcript) == 0 {
		status += "\nNo visible history"
	}
	return ansi.Hardwrap(status, model.widthOrDefault(), true) + "\n"
}
