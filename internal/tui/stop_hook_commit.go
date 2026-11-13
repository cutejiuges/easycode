package tui

import (
	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) projectStopHookCommit(e protocol.Event) (tea.Model, tea.Cmd) {
	p, err := protocol.DecodeStopHookCommitted(e)
	if err != nil {
		m.errorSummary = "stream_protocol_error: invalid stop hook commit"
		return m, nil
	}
	sample, _ := p.Target.AssistantSample()
	if old := m.stopCommit; old != nil {
		previous, _ := old.Target.AssistantSample()
		if previous.TurnID != sample.TurnID || sample.SampleIndex < previous.SampleIndex || p.DetailSequence <= old.DetailSequence {
			return m, nil
		}
		if previous == sample && (old.Phase == protocol.StopHookDecided || old.Phase == protocol.StopHookDelivered) && p.Phase != protocol.StopHookDecided && p.Phase != protocol.StopHookDelivered {
			return m, nil
		}
		if previous == sample && old.Phase == protocol.StopHookDelivered && p.Phase != protocol.StopHookDelivered {
			return m, nil
		}
		if previous == sample && old.PlanDigest != p.PlanDigest {
			m.errorSummary = "stream_protocol_error: stop plan mismatch"
			return m, nil
		}
	}
	m.stopCommit = &p
	switch p.Phase {
	case protocol.StopHookPlanned, protocol.StopHookStarted, protocol.StopHookFinished:
		m.stopStatus = "Checking completion"
	case protocol.StopHookDecided:
		if p.Disposition == domain.StopContinued {
			m.stopStatus = "Preparing continuation"
		}
	case protocol.StopHookDelivered:
		m.stopStatus = "Continuing after completion check"
	}
	b := &m.browser
	selected := false
	var cursor protocol.TranscriptCursor
	for i := range b.items {
		item := &b.items[i]
		if item.StopHook != nil && item.StopHook.Target == p.Target && item.StopHook.PlanDigest == p.PlanDigest {
			item.StopHook = &p
			item.DetailSequence = p.DetailSequence
			cursor = item.Cursor
			selected = selected || b.selected == i
		}
	}
	filtered := b.cache[:0]
	for _, page := range b.cache {
		affected := page.key.source == protocol.TranscriptStopHooks && page.key.stopTarget == p.Target
		for _, item := range page.list.Items {
			affected = affected || item.StopHook != nil && item.StopHook.Target == p.Target || p.Phase == protocol.StopHookPlanned && item.TurnID == sample.TurnID && item.Cursor.Sequence+2 == p.DetailSequence
		}
		if !affected {
			filtered = append(filtered, page)
		}
	}
	b.cache = filtered
	if m.detailOpen && !b.detail {
		if b.selected >= 0 && b.selected < len(b.items) {
			b.entryCursor = b.items[b.selected].Cursor
		}
		return m.loadTranscriptPage(b.listKey)
	}
	if m.detailOpen && b.detail && selected && cursor.Valid() && b.source == protocol.TranscriptStopHooks {
		b.invalidate()
		b.offset = 0
		b.previous = nil
		b.positions[7] = transcriptDetailPosition{}
		b.page = nil
		return m.loadTranscriptDetail()
	}
	return m, nil
}
