package tui

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func FuzzCompletionEditingInvariants(f *testing.F) {
	for _, seed := range [][]byte{{2, 3, 5, 4, 7}, {9, 12, 4, 4}, {13, 4, 15, 3, 7}, {1, 6, 4, 12, 5, 4}, {0xde, 0xad, 0xbe, 0xef}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 128 {
			return
		}
		m := completionModel(t, "$rev")
		for _, operation := range operations {
			previous := m
			matches := slices.Clone(previous.completion.matches)
			var message tea.Msg
			switch operation & 15 {
			case 0:
				message = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("$rev")}
			case 1:
				message = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/con")}
			case 2:
				message = tea.KeyMsg{Type: tea.KeySpace}
			case 3:
				message = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\u0301")}
			case 4:
				message = tea.KeyMsg{Type: tea.KeyTab}
			case 5:
				message = tea.KeyMsg{Type: tea.KeyLeft}
			case 6:
				message = tea.KeyMsg{Type: tea.KeyRight}
			case 7:
				message = tea.KeyMsg{Type: tea.KeyBackspace}
			case 8:
				message = tea.KeyMsg{Type: tea.KeyDelete}
			case 9:
				message = tea.KeyMsg{Type: tea.KeyEsc}
			case 10:
				message = tea.KeyMsg{Type: tea.KeyHome}
			case 11:
				message = tea.KeyMsg{Type: tea.KeyEnd}
			case 12:
				texts := []string{"", "\x1b\r\n\t\x03", " args \u0301", "review"}
				message = tea.KeyMsg{Type: tea.KeyEnter, Runes: []rune(texts[int(operation>>4)%len(texts)]), Paste: true}
			case 13:
				message = tea.WindowSizeMsg{Width: 1 + int(operation>>4)*8, Height: 24}
			case 14:
				message = tea.KeyMsg{Type: tea.KeyCtrlJ}
			case 15:
				message = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("👩‍💻\u3000")}
			}
			m = updateModel(t, m, message)
			c := m.composer
			if !utf8.ValidString(c.text) || c.cursor < 0 || c.cursor > len(c.text) || c.layout.source != c.text {
				t.Fatal("editing produced invalid text, cursor or layout")
			}
			index := c.pointIndex()
			if index >= len(c.layout.points) || int(c.layout.points[index].offset) != c.cursor {
				t.Fatalf("cursor %d is not a grapheme boundary in %q", c.cursor, c.text)
			}
			if !slices.Equal(previous.completion.matches, matches) {
				t.Fatal("update mutated a published candidate snapshot")
			}
			s := m.completion
			if len(s.matches) > 0 && (s.selected < 0 || s.selected >= len(s.matches)) {
				t.Fatal("selection is outside the current matches")
			}
			for _, match := range s.matches {
				if match.index < 0 || match.index >= len(s.catalog.entries) {
					t.Fatal("match refers outside the catalog")
				}
			}
			if s.rows > 0 {
				if !s.valid || s.suppression != completionNotSuppressed || m.completionBlocked() || len(s.matches) == 0 || !strings.Contains(s.view, "> ") {
					t.Fatal("invisible or ineligible selection remains active")
				}
				for _, line := range strings.Split(s.view, "\n") {
					if ansi.StringWidth(line) > m.widthOrDefault() {
						t.Fatal("completion line exceeds the terminal width")
					}
				}
			}
			if first, second := m.View(), m.View(); first != second {
				t.Fatal("render is not deterministic")
			}
		}
	})
}
