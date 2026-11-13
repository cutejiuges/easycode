package tui

import (
	"easycode/internal/domain"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type pluginBrowser struct {
	entries         []domain.PluginListEntry
	open, detail    bool
	selected, skill int
}

// WithPlugins 接收与 Skills 同次发布的最终快照；构造、导航和渲染均无 I/O。
func (m Model) WithPlugins(entries []domain.PluginListEntry) Model {
	values := make([]domain.PluginListEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Validate() != nil {
			m.errorSummary = "plugin_snapshot_invalid"
			return m
		}
		values = append(values, entry.Clone())
	}
	m.plugins = pluginBrowser{entries: values}
	return m
}
func (m Model) selectedPluginSkills() []domain.SkillListEntry {
	if len(m.plugins.entries) == 0 {
		return nil
	}
	id := m.plugins.entries[m.plugins.selected].ID
	var entries []domain.SkillListEntry
	for _, entry := range m.skills {
		if entry.Source.Scope == "plugin" && entry.Source.Directory == id {
			entries = append(entries, entry)
		}
	}
	return entries
}

func (m Model) pluginDetailRows() int {
	if len(m.plugins.entries) == 0 {
		return 0
	}
	entry := m.plugins.entries[m.plugins.selected]
	rows := len(m.selectedPluginSkills()) + len(entry.Diagnostics)
	if entry.Omitted > 0 {
		rows++
	}
	return rows
}
func (m Model) handlePluginsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Paste {
		return m, nil
	}
	switch key.Type {
	case tea.KeyCtrlC:
		m.plugins.open = false
		return m.handleKey(key)
	case tea.KeyEsc:
		if m.plugins.detail {
			m.plugins.detail = false
		} else {
			m.plugins.open = false
		}
	case tea.KeyUp:
		if m.plugins.detail {
			m.plugins.skill = max(0, m.plugins.skill-1)
		} else {
			m.plugins.selected = max(0, m.plugins.selected-1)
		}
	case tea.KeyDown:
		if m.plugins.detail {
			m.plugins.skill = min(max(0, m.pluginDetailRows()-1), m.plugins.skill+1)
		} else {
			m.plugins.selected = min(max(0, len(m.plugins.entries)-1), m.plugins.selected+1)
		}
	case tea.KeyPgUp:
		if m.plugins.detail {
			m.plugins.skill = max(0, m.plugins.skill-max(1, m.heightOrDefault()-2))
		}
	case tea.KeyPgDown:
		if m.plugins.detail {
			m.plugins.skill = min(max(0, m.pluginDetailRows()-1), m.plugins.skill+max(1, m.heightOrDefault()-2))
		}
	case tea.KeyEnter:
		if len(m.plugins.entries) == 0 {
			return m, nil
		}
		if !m.plugins.detail {
			m.plugins.detail, m.plugins.skill = true, 0
			return m, nil
		}
		entries := m.selectedPluginSkills()
		plugin := m.plugins.entries[m.plugins.selected]
		if m.plugins.skill >= len(entries) || (plugin.Status != domain.PluginAvailable && plugin.Status != domain.PluginPartial) || !explicitSkillEntry(entries[m.plugins.skill]) {
			return m, nil
		}
		text := skillInvocationText(entries[m.plugins.skill].Name)
		if err := m.composer.replace(0, len(m.composer.text), text); err != nil {
			m.errorSummary = "invalid_input: " + err.Error()
			return m, nil
		}
		m.plugins.open = false
	}
	return m, nil
}

func (m Model) renderPlugins() string {
	title := "Plugins · Skills only"
	controls := "Enter: details · Esc: back · Ctrl+C: stop"
	var lines []string
	selectedRow := 0
	if len(m.plugins.entries) == 0 {
		lines = []string{"No local plugins configured.", "Restart with --plugin-dir PATH to load a skills-only package."}
	} else if m.plugins.detail {
		entry := m.plugins.entries[m.plugins.selected]
		title = "Plugin details · Skills only"
		controls = "Enter: use in composer · Esc: back · Ctrl+C: stop"
		lines = []string{pluginLabel(entry), entry.Description}
		for i, skill := range m.selectedPluginSkills() {
			marker := "  "
			if i == m.plugins.skill {
				marker, selectedRow = "> ", len(lines)
			}
			state := skill.Status
			if skill.Status == "available" && !skill.Metadata.UserInvocable {
				state += " · model only"
			}
			if skill.Metadata.DisableModelInvocation {
				state += " · manual"
			}
			line := fmt.Sprintf("%s%s %s [%s]", marker, skill.Name, skill.Metadata.ArgumentHint, state)
			if skill.Metadata.Description != "" {
				line += " " + skill.Metadata.Description
			}
			lines = append(lines, line)
			if skill.Code != "" {
				lines = append(lines, "  "+skill.Code)
			}
		}
		for i, diagnostic := range entry.Diagnostics {
			if m.plugins.skill == len(m.selectedPluginSkills())+i {
				selectedRow = len(lines)
			}
			lines = append(lines, diagnostic.Skill+" "+diagnostic.Code)
		}
		if entry.Omitted > 0 {
			if m.plugins.skill == len(m.selectedPluginSkills())+len(entry.Diagnostics) {
				selectedRow = len(lines)
			}
			lines = append(lines, fmt.Sprintf("%d more diagnostics omitted", entry.Omitted))
		}
	} else {
		for i, entry := range m.plugins.entries {
			marker := "  "
			if i == m.plugins.selected {
				marker, selectedRow = "> ", len(lines)
			}
			lines = append(lines, marker+pluginLabel(entry))
			for _, diagnostic := range entry.Diagnostics {
				lines = append(lines, "  "+diagnostic.Code)
			}
		}
	}
	width, height := m.widthOrDefault(), m.heightOrDefault()
	if height < 3 {
		if height == 1 {
			return ansi.Truncate("Esc ^C", width, "")
		}
		return ansi.Truncate("Esc:back", width, "") + "\n" + ansi.Truncate("^C:stop", width, "")
	}
	if width < 48 {
		controls = "Esc:back · ^C:stop · Enter"
	}
	space := height - 2
	start := min(max(0, selectedRow-space+1), max(0, len(lines)-space))
	lines = append([]string{title, controls}, lines[start:min(len(lines), start+space)]...)
	for i, line := range lines {
		lines[i] = ansi.Truncate(strings.ReplaceAll(safeComposerText(line), "\n", " ↵ "), width, "")
	}
	return strings.Join(lines, "\n")
}
func pluginLabel(entry domain.PluginListEntry) string {
	name := entry.ID
	if name == "" {
		name = fmt.Sprintf("Argument %d", entry.Argument)
	}
	if entry.Version != "" {
		name += " " + entry.Version
	}
	return fmt.Sprintf("%s · %s [%s] · %d skills", name, entry.Format, entry.Status, entry.Available)
}
