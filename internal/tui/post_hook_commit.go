package tui

import (
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) projectPostHookCommit(e protocol.Event) (tea.Model, tea.Cmd) {
	p, err := protocol.DecodePostHookCommitted(e)
	if err != nil {
		m.errorSummary = "stream_protocol_error: invalid post hook commit"
		return m, nil
	}
	if m.postHooks == nil {
		m.postHooks = make(map[string]protocol.PostHookCommittedPayload)
	}
	previous, exists := m.postHooks[p.InvocationID]
	if exists && previous.PlanDigest != p.PlanDigest {
		m.errorSummary = "stream_protocol_error: post hook plan mismatch"
		return m, nil
	}
	if exists && previous.DetailSequence >= p.DetailSequence {
		return m, nil
	}
	m.postHooks[p.InvocationID] = p
	b := &m.browser
	var cursor protocol.TranscriptCursor
	selected := false
	for i := range b.items {
		if b.items[i].Activity.InvocationID == p.InvocationID {
			b.items[i].DetailSequence = p.DetailSequence
			cursor = b.items[i].Cursor
			selected = b.selected == i
		}
	}
	b.invalidateHookPages(p.InvocationID, cursor, protocol.TranscriptPostHooks)
	if m.detailOpen && !b.detail {
		return m.reloadHookList()
	}
	if m.detailOpen && b.detail && selected && b.source == protocol.TranscriptPostHooks && b.stream == "" {
		b.invalidate()
		b.offset = 0
		b.previous = nil
		b.positions[6] = transcriptDetailPosition{}
		b.page = nil
		return m.loadTranscriptDetail()
	}
	return m, nil
}
