package tui

import (
	"easycode/internal/domain"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

// WithSkills 接收启动时冻结的列表快照，不在渲染或按键时访问来源。
func (model Model) WithSkills(entries []domain.SkillListEntry) Model {
	model.skills = append([]domain.SkillListEntry(nil), entries...)
	catalog, err := newCompletionCatalog(model.skills)
	model.completion = completionState{catalog: catalog}
	if err != nil {
		model.errorSummary = "completion_unavailable: " + err.Error()
	}
	return model
}
func (model Model) handleSkillsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEsc:
		model.skillsOpen = false
	case tea.KeyUp:
		if model.skillSelection > 0 {
			model.skillSelection--
		}
	case tea.KeyDown:
		if model.skillSelection+1 < len(model.skills) {
			model.skillSelection++
		}
	case tea.KeyEnter:
		if len(model.skills) > 0 {
			entry := model.skills[model.skillSelection]
			if explicitSkillEntry(entry) {
				text := "/" + entry.Name + " "
				if err := model.composer.replace(0, len(model.composer.text), text); err == nil {
					model.skillsOpen = false
				}
			}
		}
	}
	return model, nil
}
func (model Model) renderSkills() string {
	width, height := model.widthOrDefault(), model.heightOrDefault()
	lines := []string{"Skills", "Enter: use in composer · Esc: back"}
	last := ""
	selectedRow := 0
	for i, e := range model.skills {
		source := e.Source.ID()
		if source != last {
			lines = append(lines, "", source)
			last = source
		}
		marker := "  "
		if i == model.skillSelection {
			marker = "> "
			selectedRow = len(lines)
		}
		state := e.Status
		if !e.Metadata.UserInvocable && e.Status == "available" {
			state += " · model only"
		}
		if e.Metadata.DisableModelInvocation {
			state += " · manual"
		}
		hint := ""
		if e.Metadata.ArgumentHint != "" {
			hint = " " + e.Metadata.ArgumentHint
		}
		line := fmt.Sprintf("%s/%s%s [%s]", marker, e.Name, hint, state)
		if e.Metadata.Description != "" {
			line += " " + e.Metadata.Description
		}
		lines = append(lines, line)
		if e.Code != "" {
			lines = append(lines, "  "+e.Code)
		}
	}
	if len(model.skills) == 0 {
		lines = append(lines, "", "No local skills found.")
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(strings.ReplaceAll(safeComposerText(line), "\n", " ↵ "), width, "")
	}
	if len(lines) > height {
		start := 0
		target := selectedRow
		if target > height-4 {
			start = min(target-height+4, len(lines)-height)
		}
		lines = lines[start:min(len(lines), start+height)]
	}
	return strings.Join(lines, "\n")
}
