package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type rewindChoice uint8

const (
	rewindBrowsing rewindChoice = iota
	rewindBack
	rewindProceed
)

func (choice *rewindChoice) key(key tea.KeyType) {
	switch key {
	case tea.KeyUp, tea.KeyLeft:
		*choice = rewindBack
	case tea.KeyDown, tea.KeyRight:
		*choice = rewindProceed
	case tea.KeyTab:
		if *choice == rewindBack {
			*choice = rewindProceed
		} else {
			*choice = rewindBack
		}
	}
}

func (choice rewindChoice) actions(proceed string) []string {
	if choice == rewindProceed {
		return []string{"  Back", "> " + proceed}
	}
	return []string{"> Back", "  " + proceed}
}

type rewindPicker struct {
	browseID uint64
	entries  []RewindEntry
	selected int
	choice   rewindChoice
	hasMore  bool
	failure  string
}

func (m *StartupModel) loadRewindPicker(result PickerResult) {
	m.rewind = rewindPicker{
		browseID: result.Request.ID,
		entries:  result.RewindEntries,
		selected: len(result.RewindEntries),
		hasMore:  result.HasMore,
		failure:  pickerSafe(result.Error, 1024),
	}
	m.phase = shellBrowsing
}

func (m *StartupModel) handleRewindKey(key tea.KeyMsg) {
	if key.Paste || key.Alt || key.Type == tea.KeyCtrlJ {
		return
	}
	picker := &m.rewind
	if picker.choice != rewindBrowsing {
		if key.Type == tea.KeyEsc || key.Type == tea.KeyEnter && picker.choice == rewindBack {
			picker.choice = rewindBrowsing
			return
		}
		if !picker.confirmFits(m.width, m.height) {
			picker.choice = rewindBack
			return
		}
		if key.Type == tea.KeyEnter {
			browse := m.request
			if m.submitPicker(PickerRequest{
				Operation: PickerBranch, Source: browse.Source, BrowseID: picker.browseID,
				RewindSelection: picker.entries[picker.selected].RewindSelection,
			}) {
				m.phase = shellRecovering
				return
			}
			// 请求未被接纳，仍按最后接纳的浏览请求释放许可。
			m.request = browse
			m.cancelNotice = "Rewind selection is no longer available"
			m.cancelResume()
			return
		}
		picker.choice.key(key.Type)
		return
	}
	switch key.Type {
	case tea.KeyEsc:
		m.cancelResume()
	case tea.KeyEnter:
		if picker.selected == len(picker.entries) {
			m.cancelResume()
			return
		}
		picker.choice = rewindBack
	case tea.KeyUp:
		picker.selected = max(0, picker.selected-1)
	case tea.KeyDown:
		picker.selected = min(len(picker.entries), picker.selected+1)
	case tea.KeyPgUp:
		picker.selected = max(0, picker.selected-picker.pageRows(m.height))
	case tea.KeyPgDown:
		picker.selected = min(len(picker.entries), picker.selected+picker.pageRows(m.height))
	}
}

func (p rewindPicker) listNotice() string {
	switch {
	case p.failure != "":
		return p.failure
	case len(p.entries) == 0:
		return "No eligible historical turns"
	case p.hasMore:
		return "Showing the latest 200 eligible turns"
	default:
		return ""
	}
}

func (p rewindPicker) pageRows(height int) int {
	rows := height - 2
	if p.listNotice() != "" {
		rows--
	}
	return max(1, rows)
}

func (p rewindPicker) view(width, height int) string {
	if p.choice != rewindBrowsing {
		return p.confirmView(width, height)
	}
	notice := p.listNotice()
	lines := make([]string, 0, min(height, len(p.entries)+4))
	if height > 1 {
		lines = append(lines, "Rewind conversation")
	}
	if height > 2 && notice != "" {
		lines = append(lines, notice)
	}
	count := p.pageRows(height)
	start := max(0, p.selected-count+1)
	for i := start; i <= len(p.entries) && i < start+count; i++ {
		marker := "  "
		if i == p.selected {
			marker = "> "
		}
		label := "Current"
		if i < len(p.entries) {
			entry := p.entries[i]
			label = fmt.Sprintf("Turn %d · %s", entry.Number, pickerSafe(entry.Summary, 512))
		}
		lines = append(lines, ansi.Truncate(marker+label, width, "…"))
	}
	if len(lines) < height {
		lines = append(lines, "↑↓ select · Enter continue · Esc back")
	}
	return fitPickerLines(lines, width)
}

const rewindCreateLabel = "Branch before this prompt"

func (p rewindPicker) confirmDetails(width int) []string {
	entry := p.entries[p.selected]
	text := fmt.Sprintf("Turn %d\nCreate and enter a branch before this turn.\nThe branch uses the current workspace files.", entry.Number)
	if entry.HasMultipleInputs {
		text += "\nOnly the first initial input will be filled in."
	}
	return strings.Split(ansi.Hardwrap(text, width, true), "\n")
}

func (p rewindPicker) confirmFits(width, height int) bool {
	return width >= len("> "+rewindCreateLabel) && height >= len(p.confirmDetails(width))+3
}

func (p rewindPicker) confirmView(width, height int) string {
	if !p.confirmFits(width, height) {
		return pickerWrapped("> Back\nResize to confirm this branch\nEsc: back", width, height, 0)
	}
	lines := p.confirmDetails(width)
	preview := strings.Split(ansi.Hardwrap(pickerSafe(p.entries[p.selected].Summary, 512), width, true), "\n")
	rows := min(len(preview), height-len(lines)-2)
	if rows < len(preview) {
		preview[rows-1] = ansi.Truncate(preview[rows-1], width-1, "") + "…"
	}
	lines = append(lines, preview[:rows]...)
	lines = append(lines, p.choice.actions(rewindCreateLabel)...)
	if len(lines) < height {
		lines = append(lines, "↑↓ choose · Enter confirm · Esc back")
	}
	return fitPickerLines(lines, width)
}
