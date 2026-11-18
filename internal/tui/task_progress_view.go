package tui

import (
	"fmt"
	"strings"

	"easycode/internal/domain"
	"github.com/charmbracelet/x/ansi"
)

func taskStatusMark(status domain.TaskStatus) string {
	switch status {
	case domain.TaskCompleted:
		return "[x]"
	case domain.TaskInProgress:
		return "[>]"
	default:
		return "[ ]"
	}
}

func (model Model) taskInlineLines(space, completionRows int) []string {
	view := model.tasks
	if !view.enabled || space < 1 || model.heightOrDefault() <= 5 {
		return nil
	}
	width := max(1, model.widthOrDefault())
	line := func(text string) string { return ansi.Truncate(text, width, "…") }
	if view.snapshot == nil {
		if view.hydration == taskHydrationFailed {
			return []string{line("Tasks: unavailable · /tasks to retry")}
		}
		return []string{line("Tasks: loading")}
	}
	p := view.snapshot
	if p.Counts.Total == 0 {
		if view.hydration == taskHydrationFailed {
			return []string{line("Tasks: unavailable · /tasks to retry")}
		}
		return nil
	}
	title := fmt.Sprintf("Tasks: %d/%d done · model report", p.Counts.Completed, p.Counts.Total)
	if view.hydration == taskHydrationFailed {
		title += " · unavailable"
	}
	if width >= 64 {
		title += " · Ctrl+T"
	}
	lines := []string{line(title)}
	if !view.expanded || completionRows > 0 || space < 3 || width < 32 {
		return lines
	}
	update, _ := p.Progress.Update()
	items := update.Items()
	count := min(6, len(items), space-1)
	if count < len(items) {
		count = min(count, space-2)
	}
	if count < 1 {
		return lines
	}
	start := 0
	for i, item := range items {
		if item.Status() == domain.TaskInProgress {
			start = max(0, min(i-count/2, len(items)-count))
			break
		}
	}
	for _, item := range items[start : start+count] {
		text := item.Content()
		if item.Status() == domain.TaskInProgress && item.ActiveForm() != "" {
			text = item.ActiveForm()
		}
		lines = append(lines, line("  "+taskStatusMark(item.Status())+" "+safeComposerText(text)))
	}
	if count < len(items) {
		lines = append(lines, line(fmt.Sprintf("  %d more · /tasks for full list", len(items)-count)))
	}
	return lines
}

type taskPanelRow struct {
	text  string
	label string
}

// taskPanelRows 仅从当前有界值派生安全阅读行，不读取历史或外部资源。
func (model Model) taskPanelRows() []taskPanelRow {
	width := max(1, model.widthOrDefault())
	var rows []taskPanelRow
	appendText := func(text, label string) {
		for _, line := range strings.Split(safeComposerText(text), "\n") {
			for _, wrapped := range strings.Split(ansi.Hardwrap(line, width, true), "\n") {
				rows = append(rows, taskPanelRow{ansi.Truncate(wrapped, width, ""), label})
			}
		}
	}
	if model.tasks.errorText != "" {
		appendText("Unavailable: "+model.tasks.errorText, "")
	}
	if model.tasks.snapshot == nil {
		if model.tasks.hydration != taskHydrationFailed {
			appendText("Loading current task progress…", "")
		}
		return rows
	}
	p := model.tasks.snapshot
	switch p.Progress.Kind() {
	case domain.TaskProgressNone:
		appendText("No task update has been saved.", "")
	case domain.TaskProgressClear:
		appendText("The task list was explicitly cleared.", "")
	case domain.TaskProgressSnapshot:
		appendText(fmt.Sprintf("%d/%d completed · %d in progress · %d pending", p.Counts.Completed, p.Counts.Total, p.Counts.InProgress, p.Counts.Pending), "")
	}
	if update, ok := p.Progress.Update(); ok {
		for i, item := range update.Items() {
			appendText(fmt.Sprintf("%d. %s [%s] %s", i+1, taskStatusMark(item.Status()), item.Status(), item.Content()), item.Content())
			if item.ActiveForm() != "" {
				appendText("   Active: "+item.ActiveForm(), item.Content())
			}
		}
		if update.Explanation() != "" {
			appendText("Update note:\n"+update.Explanation(), "")
		}
	}
	return rows
}

func (model Model) taskPanelScrollLimit() int {
	return max(0, len(model.taskPanelRows())-max(1, model.heightOrDefault()-2))
}

func (model Model) taskPanelAnchor() string {
	if !model.tasks.open {
		return ""
	}
	rows := model.taskPanelRows()
	if model.tasks.scroll < len(rows) {
		return rows[model.tasks.scroll].label
	}
	return ""
}

func (model *Model) restoreTaskPanelAnchor(label string) {
	if label != "" {
		for i, row := range model.taskPanelRows() {
			if row.label == label {
				model.tasks.scroll = min(i, model.taskPanelScrollLimit())
				return
			}
		}
	}
	model.tasks.scroll = min(model.tasks.scroll, model.taskPanelScrollLimit())
}

func (model Model) renderTasksPanel() string {
	width, height := max(1, model.widthOrDefault()), max(1, model.heightOrDefault())
	lines := []string{ansi.Truncate("Tasks · model-reported progress", width, "…")}
	rows := model.taskPanelRows()
	space := max(0, height-2)
	start := min(model.tasks.scroll, max(0, len(rows)-space))
	for _, row := range rows[start:min(len(rows), start+space)] {
		lines = append(lines, row.text)
	}
	if height > 1 {
		lines = append(lines, ansi.Truncate("Up/Down: scroll · r: refresh · Esc: back", width, "…"))
	}
	return strings.Join(lines, "\n")
}
