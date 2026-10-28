package tui

import (
	"reflect"
	"strings"
	"testing"

	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func composerWithText(t *testing.T, text string) composer {
	t.Helper()
	c := newComposer()
	if err := c.replace(0, 0, text); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestComposerGraphemeEditingAndSnapshots(t *testing.T) {
	for _, cluster := range []string{"中", "e\u0301", "👨‍👩‍👧‍👦", "👍🏽", "🇨🇳"} {
		t.Run(cluster, func(t *testing.T) {
			c := composerWithText(t, "a"+cluster+"z")
			snapshot := c
			c.horizontal(-1)
			c.horizontal(-1)
			if c.cursor != 1 {
				t.Fatalf("cursor=%d", c.cursor)
			}
			if err := c.replace(c.cursor, c.cursor, "X"); err != nil {
				t.Fatal(err)
			}
			if c.text != "aX"+cluster+"z" {
				t.Fatal(c.text)
			}
			if err := c.erase(false); err != nil {
				t.Fatal(err)
			}
			if c.text != "aXz" {
				t.Fatal(c.text)
			}
			if err := c.erase(true); err != nil {
				t.Fatal(err)
			}
			if c.text != "az" || snapshot.text != "a"+cluster+"z" {
				t.Fatal("snapshot changed")
			}
		})
	}
	c := composerWithText(t, "e")
	if err := c.replace(1, 1, "\u0301"); err != nil {
		t.Fatal(err)
	}
	c.horizontal(-1)
	if c.cursor != 0 {
		t.Fatal("combining sequence split")
	}
}

func TestComposerLogicalAndVisualMovement(t *testing.T) {
	c := composerWithText(t, "abcdef\nx\nabcdef\n")
	c.moveTo(5)
	layout := c.layout
	c.vertical(1)
	if c.cursor != 8 {
		t.Fatalf("short line cursor=%d", c.cursor)
	}
	c.vertical(1)
	if c.cursor != 14 {
		t.Fatalf("preferred column lost: %d", c.cursor)
	}
	if c.layout != layout {
		t.Fatal("movement rebuilt layout")
	}
	c.input(tea.KeyMsg{Type: tea.KeyHome})
	if c.cursor != 9 {
		t.Fatal(c.cursor)
	}
	c.input(tea.KeyMsg{Type: tea.KeyEnd})
	if c.cursor != 15 {
		t.Fatal(c.cursor)
	}
	c.resize(7, 6)
	if c.cursor != 15 || c.preferred != -1 {
		t.Fatal("resize moved cursor")
	}
	c.moveTo(0)
	c.horizontal(-1)
	c.erase(true)
	if c.cursor != 0 {
		t.Fatal("start boundary")
	}
	c.moveTo(len(c.text))
	c.horizontal(1)
	c.erase(false)
	if c.cursor != len(c.text) {
		t.Fatal("end boundary")
	}
}

func TestComposerHistoryNavigationAndBudgets(t *testing.T) {
	c := composerWithText(t, "  draft  ")
	c.moveTo(2)
	c.history.add("one")
	c.history.add("two")
	c.history.add("two")
	snapshot := c
	c.vertical(-1)
	if c.text != "two" {
		t.Fatal(c.text)
	}
	c.vertical(-1)
	if c.text != "one" {
		t.Fatal(c.text)
	}
	c.vertical(1)
	c.vertical(1)
	if c.text != "  draft  " || c.cursor != 2 {
		t.Fatalf("draft lost: %#v", c)
	}
	c.vertical(-1)
	c.replace(c.cursor, c.cursor, "!")
	if c.history.browsing || c.text != "two!" || c.history.entries[1] != "two" {
		t.Fatal("recall edit mutated history")
	}
	c.history.add("three")
	if len(snapshot.history.entries) != 2 {
		t.Fatal("history snapshot changed")
	}
	for i := 0; i < 110; i++ {
		c.history.add(strings.Repeat("x", i+1))
	}
	if len(c.history.entries) != 100 {
		t.Fatal(len(c.history.entries))
	}
	h := composerHistory{}
	big := strings.Repeat("x", protocol.MaxInputBytes)
	h.add(big)
	h.add("y" + big[1:])
	h.add("z" + big[1:])
	if len(h.entries) != 2 || h.bytes != 8<<20 || h.entries[0][0] != 'y' {
		t.Fatal("byte eviction failed")
	}
	before := h
	h.add(big + "x")
	h.add(" \n")
	if !reflect.DeepEqual(h, before) {
		t.Fatal("invalid seed changed history")
	}
}

func TestComposerHistoryWrapAndWhitespaceDraft(t *testing.T) {
	c := composerWithText(t, " \n ")
	c.moveTo(0)
	c.resize(8, 5)
	c.history.add("abcdefgh\nsecond")
	c.vertical(-1)
	if c.rowIndex() != 0 {
		t.Fatal("recall did not land on first visual row")
	}
	for c.rowIndex() < len(c.layout.rows)-1 {
		c.vertical(1)
	}
	c.vertical(1)
	if c.text != " \n " || c.cursor != 0 {
		t.Fatal("whitespace draft lost")
	}
}
