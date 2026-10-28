package tui

import (
	"reflect"
	"strings"
	"testing"

	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func TestComposerInputNewlinesAndSubmission(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter, Alt: true}, {Type: tea.KeyCtrlJ}} {
		c := composerWithText(t, "  code ")
		intent, err := c.input(key)
		if err != nil || intent != composerEdited || c.text != "  code \n" {
			t.Fatalf("newline: %q %v", c.text, err)
		}
		intent, err = c.input(tea.KeyMsg{Type: tea.KeyEnter})
		if err != nil || intent != composerSubmit || c.text != "  code \n" {
			t.Fatal("submit changed text")
		}
	}
	c := composerWithText(t, "a\\b")
	c.moveTo(2)
	intent, _ := c.input(tea.KeyMsg{Type: tea.KeyEnter})
	if intent != composerEdited || c.text != "a\nb" {
		t.Fatal(c.text)
	}
	c = composerWithText(t, " \n\t")
	intent, _ = c.input(tea.KeyMsg{Type: tea.KeyEnter})
	if intent != composerEdited {
		t.Fatal("blank submitted")
	}
	for _, key := range []tea.KeyType{tea.KeyTab, tea.KeyEsc, tea.KeyCtrlC} {
		old := c
		c.input(tea.KeyMsg{Type: key})
		if !reflect.DeepEqual(c, old) {
			t.Fatal("non-editing key changed draft")
		}
	}
}

func TestComposerPasteAtomicLimitAndControlCharacters(t *testing.T) {
	c := composerWithText(t, "ab")
	c.moveTo(1)
	paste := "x\r\ny\rz\n\t\x1b\x03"
	intent, err := c.input(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(paste), Paste: true})
	if err != nil || intent != composerEdited || c.text != "ax\ny\nz\n\t\x1b\x03b" {
		t.Fatalf("paste=%q err=%v", c.text, err)
	}
	c = composerWithText(t, strings.Repeat("x", protocol.MaxInputBytes))
	c.history.add("history")
	snapshot := c
	_, err = c.input(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("中"), Paste: true})
	if err == nil || !reflect.DeepEqual(c, snapshot) {
		t.Fatal("oversized paste was not atomic")
	}
	intent, err = c.input(tea.KeyMsg{Type: tea.KeyEnter})
	if err != nil || intent != composerSubmit {
		t.Fatal("exact limit rejected")
	}
	snapshot = c
	c.input(tea.KeyMsg{Type: tea.KeyRunes, Paste: true})
	if !reflect.DeepEqual(c, snapshot) {
		t.Fatal("empty paste changed draft")
	}
}
