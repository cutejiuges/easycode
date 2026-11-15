package tui

import (
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

// projectPreInput 按真实提交序列更新投影；迟到 checking 无法覆盖最终选择。
func (m Model) projectPreInput(p protocol.HookActivityPayload) (tea.Model, tea.Cmd) {
	if m.preInputs == nil {
		m.preInputs = make(map[string]protocol.HookActivityPayload)
	}
	id := p.InvocationID()
	previous, exists := m.preInputs[id]
	if exists {
		if previous.PlanDigest != p.PlanDigest {
			m.errorSummary = "stream_protocol_error: pre hook plan mismatch"
			return m, nil
		}
		if previous.PreInput.Sequence() >= p.PreInput.Sequence() || previous.PreInput.Kind() == protocol.PreInputSelected || previous.PreInput.Kind() == protocol.PreInputRejected {
			return m, nil
		}
	}
	if index, ok := m.toolItems[id]; ok && index < len(m.transcript) && !m.toolClosed[id] {
		m.transcript[index].hook = &p
		if p.PreInput.Kind() == protocol.PreInputSelected && m.transcript[index].activity != nil {
			activity := *m.transcript[index].activity
			activity.Summary = p.PreInput.Summary()
			m.transcript[index].activity = &activity
		}
	}
	if p.PreInput.Kind() == protocol.PreInputNone {
		return m, nil
	}
	m.preInputs[id] = p
	b := &m.browser
	var cursor protocol.TranscriptCursor
	selected := false
	for i := range b.items {
		if b.items[i].Activity.InvocationID == id {
			b.items[i].PreHook = &p
			b.items[i].DetailSequence = max(b.items[i].DetailSequence, p.PreInput.Sequence())
			if p.PreInput.Kind() == protocol.PreInputSelected {
				b.items[i].Activity.Summary = p.PreInput.Summary()
			}
			cursor = b.items[i].Cursor
			selected = b.selected == i
		}
	}
	filtered := b.cache[:0]
	for _, cached := range b.cache {
		affected := cached.key.cursor == cursor && cached.key.source == protocol.TranscriptPreHooks
		for _, item := range cached.list.Items {
			affected = affected || item.Activity.InvocationID == id
		}
		if !affected {
			filtered = append(filtered, cached)
		}
	}
	b.cache = filtered
	if m.detailOpen && !b.detail {
		if b.selected >= 0 && b.selected < len(b.items) {
			b.entryCursor = b.items[b.selected].Cursor
		}
		b.invalidate()
		return m.loadTranscriptPage(b.listKey)
	}
	if m.detailOpen && b.detail && selected && b.source == protocol.TranscriptPreHooks && b.stream == "" {
		b.invalidate()
		b.page = nil
		scroll := m.detailScroll
		updated, cmd := m.loadTranscriptDetail()
		next := updated.(Model)
		next.detailScroll = scroll
		return next, cmd
	}
	return m, nil
}
