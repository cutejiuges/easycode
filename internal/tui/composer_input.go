package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type composerIntent uint8

const (
	composerEdited composerIntent = iota
	composerSubmit
)

func (c *composer) newlineStart(key tea.KeyMsg) (int, bool) {
	if key.Paste {
		return 0, false
	}
	switch key.Type {
	case tea.KeyCtrlJ:
		return c.cursor, true
	case tea.KeyEnter:
		if key.Alt {
			return c.cursor, true
		}
		index := c.pointIndex()
		if index > 0 && c.text[int(c.layout.points[index-1].offset):c.cursor] == "\\" {
			return c.cursor - 1, true
		}
	}
	return 0, false
}

func (c *composer) input(key tea.KeyMsg) (composerIntent, error) {
	if key.Paste {
		return composerEdited, c.replace(c.cursor, c.cursor, string(key.Runes))
	}
	if start, newline := c.newlineStart(key); newline {
		return composerEdited, c.replace(start, c.cursor, "\n")
	}
	switch key.Type {
	case tea.KeyEnter:
		if strings.TrimSpace(c.text) != "" {
			return composerSubmit, nil
		}
	case tea.KeyLeft, tea.KeyCtrlB:
		c.horizontal(-1)
	case tea.KeyRight, tea.KeyCtrlF:
		c.horizontal(1)
	case tea.KeyUp:
		c.vertical(-1)
	case tea.KeyDown:
		c.vertical(1)
	case tea.KeyHome, tea.KeyCtrlA:
		c.moveTo(strings.LastIndex(c.text[:c.cursor], "\n") + 1)
	case tea.KeyEnd, tea.KeyCtrlE:
		offset := strings.Index(c.text[c.cursor:], "\n")
		if offset < 0 {
			offset = len(c.text) - c.cursor
		}
		c.moveTo(c.cursor + offset)
	case tea.KeyBackspace:
		return composerEdited, c.erase(true)
	case tea.KeyDelete:
		return composerEdited, c.erase(false)
	case tea.KeySpace:
		if !key.Alt {
			return composerEdited, c.replace(c.cursor, c.cursor, " ")
		}
	case tea.KeyRunes:
		if !key.Alt {
			return composerEdited, c.replace(c.cursor, c.cursor, string(key.Runes))
		}
	}
	return composerEdited, nil
}
