package tui

import (
	"strings"
	"unicode/utf8"

	"easycode/internal/protocol"
)

type composerDraft struct {
	text   string
	cursor int
}

type composerHistory struct {
	entries  []string
	bytes    int
	browsing bool
	index    int
	saved    composerDraft
}

func (h *composerHistory) endNavigation() {
	h.browsing = false
	h.index = 0
	h.saved = composerDraft{}
}

func (h *composerHistory) add(text string) {
	text = normalizeComposerText(text)
	if !utf8.ValidString(text) || len(text) > protocol.MaxInputBytes || strings.TrimSpace(text) == "" {
		return
	}
	h.endNavigation()
	if len(h.entries) > 0 && h.entries[len(h.entries)-1] == text {
		return
	}
	entries := append(append([]string(nil), h.entries...), text)
	size := h.bytes + len(text)
	start := 0
	for len(entries)-start > 100 || size > 8<<20 {
		size -= len(entries[start])
		start++
	}
	h.entries = append([]string(nil), entries[start:]...)
	h.bytes = size
}

func (c *composer) recall(delta int) {
	h := &c.history
	if len(h.entries) == 0 {
		return
	}
	if !h.browsing {
		if delta > 0 {
			return
		}
		h.saved = composerDraft{c.text, c.cursor}
		h.index = len(h.entries)
		h.browsing = true
	}
	target := h.index + delta
	if target < 0 {
		return
	}
	if target == len(h.entries) {
		c.text, c.cursor = h.saved.text, h.saved.cursor
		h.endNavigation()
	} else {
		h.index = target
		c.text = h.entries[target]
		c.cursor = len(c.text)
	}
	c.preferred = -1
	c.relayout()
	if h.browsing && delta < 0 {
		c.cursor = int(c.layout.points[c.layout.rows[0].last-1].offset)
	}
	c.reveal()
}
