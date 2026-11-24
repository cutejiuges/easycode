package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func renderedChatRows(value string) int {
	if value == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(value, "\n"), "\n") + 1
}

// nonEffortStatusRows 为主线、通知和补全共用现有状态的真实行预算。
func (model Model) nonEffortStatusRows() int {
	rows := renderedChatRows(model.handoffStatus())
	for _, present := range []bool{model.cancelling, model.planReview.starting != "", model.stopStatus != "", model.reasoning.notice != "", model.reasoningStatus() != "", model.reading.detached || model.reading.notice != "" || model.reading.newContent, model.errorSummary != "", model.title.value != "", model.title.status() != ""} {
		if present {
			rows++
		}
	}
	if model.compactionStatus != "" {
		rows += renderedChatRows(ansi.Hardwrap(model.compactionStatus, model.widthOrDefault(), true))
	}
	return rows
}

func (model Model) composerChatRows() int {
	return renderedChatRows(model.composerWorkModeView(model.composer.view(true)))
}

// chatContentRows 优先预留输入、队列、固定标题和分隔，剩余空间由正文及短通知使用。
func (model Model) chatContentRows() int {
	return max(0, model.heightOrDefault()-model.composerChatRows()-renderedChatRows(model.renderInputs())-model.nonEffortStatusRows()-3)
}

// candidateRows 统一计入互斥的命令与文件候选，避免任务区重复占用其行。
func (model Model) candidateRows() int { return model.completion.rows + model.files.rows }

type chatAuxiliaryLayout struct {
	space, completionRows, effortRows int
	taskLines                         []string
}

// auxiliaryLayout 让活动补全先使用共享余量，通知和任务只分配尚未使用的行。
func (model Model) auxiliaryLayout(completionRows int) chatAuxiliaryLayout {
	layout := chatAuxiliaryLayout{space: model.chatContentRows(), completionRows: completionRows}
	remaining := max(0, layout.space-completionRows)
	layout.effortRows = min(len(model.effortNotice), 6, max(0, remaining-1))
	remaining -= layout.effortRows
	layout.taskLines = model.taskInlineLines(remaining, completionRows)
	return layout
}

func (model Model) renderTaskInline() string {
	lines := model.auxiliaryLayout(model.candidateRows()).taskLines
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// fitChatView 在极端状态组合下裁去空行和较后的主线内容，始终保留已预算的输入区。
func (model Model) fitChatView(prefix, footer string) string {
	height, width := max(1, model.heightOrDefault()), max(1, model.widthOrDefault())
	if renderedChatRows(footer) > height {
		return model.composerWorkModeView(model.composer.view(model.chatComposerEditable()))
	}
	lines := strings.Split(strings.TrimSuffix(prefix, "\n"), "\n")
	capacity := height - renderedChatRows(footer)
	if len(lines) > capacity {
		compact := make([]string, 0, len(lines))
		remove := len(lines) - capacity
		for _, line := range lines {
			if line == "" && remove > 0 {
				remove--
				continue
			}
			compact = append(compact, line)
		}
		lines = compact[:min(len(compact), capacity)]
	}
	if footer != "" {
		lines = append(lines, strings.Split(strings.TrimSuffix(footer, "\n"), "\n")...)
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "")
	}
	return strings.Join(lines, "\n") + "\n"
}
