package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

// reasoningKey 区分临时作用域与 durable cursor，绝不按正文匹配。
type reasoningKey struct {
	turn          domain.TurnID
	sample, block uint32
	live          bool
}
type transcriptDetailItem struct {
	protocol.TranscriptListItem
	temporary reasoningKey
}

func detailItems(items []protocol.TranscriptListItem) []transcriptDetailItem {
	result := make([]transcriptDetailItem, len(items))
	for i, item := range items {
		result[i].TranscriptListItem = item
	}
	return result
}

type reasoningPreview struct {
	key           reasoningKey
	cursor        protocol.TranscriptCursor
	source        domain.ReasoningSource
	text          string
	offset, total uint64
	availability  domain.ReasoningAvailability
	touched       uint64
}

// activeReasoning 持有一个 sample 共用的显示预算；严格校验状态独立于淘汰。
type activeReasoning struct {
	stream      protocol.ReasoningStreamState
	sample      uint32
	items       []reasoningPreview
	tick        uint64
	status      string
	failed      bool
	notice      string
	disposition protocol.SampleDisposition
}

func (m *Model) selectedReasoning() reasoningKey {
	b := &m.browser
	if m.detailOpen && b.selected >= 0 && b.selected < len(b.items) {
		return b.items[b.selected].temporary
	}
	return reasoningKey{}
}
func (a *activeReasoning) find(key reasoningKey) *reasoningPreview {
	for i := range a.items {
		if a.items[i].key == key {
			return &a.items[i]
		}
	}
	return nil
}
func (a *activeReasoning) limit(selected reasoningKey) {
	for len(a.items) > 50 {
		victim := -1
		for i, item := range a.items {
			if item.key == selected {
				continue
			}
			if victim < 0 || item.touched < a.items[victim].touched {
				victim = i
			}
		}
		if victim < 0 {
			break
		}
		a.items = append(a.items[:victim:victim], a.items[victim+1:]...)
	}
	total := 0
	for _, item := range a.items {
		total += len(item.text)
	}
	for total > protocol.MaxReasoningPreviewBytes {
		victim := -1
		for i, item := range a.items {
			if item.key == selected || item.text == "" {
				continue
			}
			if victim < 0 || item.touched < a.items[victim].touched {
				victim = i
			}
		}
		if victim < 0 {
			for i, item := range a.items {
				if item.text != "" {
					victim = i
					break
				}
			}
		}
		if victim < 0 {
			break
		}
		item := &a.items[victim]
		drop := min(len(item.text), total-protocol.MaxReasoningPreviewBytes)
		for drop < len(item.text) && !utf8.RuneStart(item.text[drop]) {
			drop++
		}
		item.text = strings.Clone(item.text[drop:])
		item.offset += uint64(drop)
		total -= drop
	}
}
func (m *Model) acceptReasoning(event protocol.Event) error {
	payload, err := protocol.DecodeAssistantReasoning(event)
	if err != nil {
		return err
	}
	if event.TurnID != m.activeTurnID {
		return fmt.Errorf("reasoning turn mismatch")
	}
	a := &m.reasoning
	if payload.SampleIndex != a.sample {
		return fmt.Errorf("reasoning sample is stale or unknown")
	}
	if err := a.stream.Accept(payload); err != nil {
		return err
	}
	key := reasoningKey{event.TurnID, payload.SampleIndex, payload.Block, true}
	a.tick++
	if payload.Stage == protocol.ReasoningStarted {
		a.items = append(a.items, reasoningPreview{key: key, source: payload.Source, touched: a.tick, availability: domain.ReasoningUnavailable})
	}
	item := a.find(key)
	if item != nil {
		item.touched = a.tick
		switch payload.Stage {
		case protocol.ReasoningDelta:
			item.text += payload.Delta.Text
			item.total = payload.Delta.Offset + uint64(len(payload.Delta.Text))
			item.availability = domain.ReasoningReadable
		case protocol.ReasoningCompleted:
			c := payload.Completion
			item.text = c.Text
			item.offset = c.Offset
			item.total = c.TotalBytes
			item.availability = c.Availability
		}
	}
	a.limit(m.selectedReasoning())
	a.status = "Thinking · Ctrl+O to inspect"
	all := a.stream.Complete() == nil
	if all {
		a.status = "Reasoning complete · waiting to save · Ctrl+O"
	}
	m.assistantItem = -1
	m.mergeLiveReasoning()
	return nil
}
func (m *Model) mergeLiveReasoning() {
	b := &m.browser
	if !m.detailOpen {
		return
	}
	selected := m.selectedReasoning()
	var selectedCursor protocol.TranscriptCursor
	if b.selected >= 0 && b.selected < len(b.items) {
		selectedCursor = b.items[b.selected].Cursor
	}
	if b.detail && b.selected >= 0 && b.selected < len(b.items) && m.reasoningPreviewForItem(b.items[b.selected]) != nil {
		m.prepareLiveReasoningPage()
		return
	}
	if b.detail || b.before.Valid() {
		return
	}
	if b.preferLive {
		selectedCursor = protocol.TranscriptCursor{}
		b.selected = 0
		b.preferLive = false
	}
	kept := b.items[:0]
	for _, item := range b.items {
		if !item.temporary.live {
			kept = append(kept, item)
		}
	}
	b.items = kept
	live := make([]transcriptDetailItem, 0, len(m.reasoning.items))
	for i := len(m.reasoning.items) - 1; i >= 0; i-- {
		item := m.reasoning.items[i]
		if item.cursor.Valid() {
			exists := false
			for _, durable := range b.items {
				if durable.Cursor == item.cursor {
					exists = true
					break
				}
			}
			if exists {
				continue
			}
			live = append(live, transcriptDetailItem{TranscriptListItem: protocol.TranscriptListItem{Cursor: item.cursor, Kind: domain.TranscriptReasoning, TurnID: item.key.turn, Reasoning: &protocol.ReasoningSummaryPayload{Source: item.source, Availability: item.availability, TotalBytes: item.total}}})
			continue
		}
		live = append(live, transcriptDetailItem{TranscriptListItem: protocol.TranscriptListItem{Kind: domain.TranscriptReasoning, TurnID: item.key.turn, Reasoning: &protocol.ReasoningSummaryPayload{Source: item.source, Availability: item.availability, TotalBytes: item.total}}, temporary: item.key})
	}
	b.items = append(live, b.items...)
	for i, item := range b.items {
		if selected.live && item.temporary == selected || !selected.live && selectedCursor.Valid() && item.Cursor == selectedCursor {
			b.selected = i
			break
		}
	}
}
func (m *Model) prepareLiveReasoningPage() {
	b := &m.browser
	var item *reasoningPreview
	if b.selected >= 0 && b.selected < len(b.items) {
		item = m.reasoningPreviewForItem(b.items[b.selected])
	}
	if item == nil {
		b.page = nil
		b.errorText = "Live excerpt no longer retained"
		return
	}
	b.offset = item.offset
	// Text 共享同一不可变字符串，不额外复制活动正文或放进查询缓存。
	b.page = &cachedTranscriptPage{body: protocol.TranscriptBodyPage{Text: item.text, Offset: item.offset, NextOffset: item.offset + uint64(len(item.text)), TotalBytes: item.total}}
	b.errorText = ""
	if item.cursor.Valid() && m.reasoning.disposition == protocol.SampleDurable {
		b.errorText = "Saved · display refresh failed · cached excerpt"
	}
	if item.availability == domain.ReasoningUnavailable {
		b.page = nil
		b.errorText = "Reasoning text unavailable"
	}
}
func (m Model) commitReasoning(payload protocol.TranscriptCommittedPayload) (Model, tea.Cmd, error) {
	if payload.Origin != protocol.TranscriptCommitSample {
		return m, nil, nil
	}
	if payload.Sample.SampleIndex != m.reasoning.sample {
		return m, nil, fmt.Errorf("reasoning commit sample is stale or unknown")
	}
	if err := m.reasoning.stream.Commit(*payload.Sample); err != nil {
		return m, nil, err
	}
	m.reasoning.sample++
	b := &m.browser
	selected := m.selectedReasoning()
	m.upgradeReasoning(*payload.Sample, payload.Sequence)
	m.reasoning.items = nil
	m.reasoning.status = ""
	if selected.live && b.detail && b.selected < len(b.items) && b.items[b.selected].Cursor.Valid() {
		previousPage, previousMarkdown, scroll := b.page, b.markdown, m.detailScroll
		b.invalidate()
		updated, cmd := m.loadTranscriptDetail()
		next := updated.(Model)
		// 保存成功后的查询可延迟；当前有界画面和源锚点持续保留到新页接管。
		next.browser.page, next.browser.markdown, next.detailScroll = previousPage, previousMarkdown, scroll
		return next, cmd, nil
	}
	return m, nil, nil
}
func (m *Model) resolveReasoning(payload protocol.TurnFailedPayload) error {
	if payload.Resolution != nil {
		if payload.Resolution.SampleIndex != m.reasoning.sample {
			return fmt.Errorf("reasoning failure sample is stale or unknown")
		}
		observed := m.reasoning.stream.Pending()
		m.reasoning.disposition = payload.Resolution.Disposition
		if err := m.reasoning.stream.Resolve(*payload.Resolution); err != nil {
			return err
		}
		if !observed {
			m.reasoning.status = ""
			m.reasoning.failed = false
			return nil
		}
		switch payload.Resolution.Disposition {
		case protocol.SampleNotCommitted:
			m.reasoning.status = "Reasoning not saved · Ctrl+O to inspect"
		case protocol.SampleUncertain:
			m.reasoning.status = "Reasoning save status uncertain · Ctrl+O to inspect"
		case protocol.SampleDurable:
			m.upgradeReasoning(*payload.Resolution.Committed.Sample, payload.Resolution.Committed.Sequence)
			m.reasoning.status = "Reasoning saved · display refresh failed · Ctrl+O"
		}
		m.reasoning.failed = true
	} else if err := m.reasoning.stream.Complete(); err != nil {
		return err
	}
	return nil
}
func (m Model) reasoningStatus() string {
	if m.cancelling || len(m.toolActivities) > 0 || m.compactionStatus != "" {
		return ""
	}
	return m.reasoning.status
}

// upgradeReasoning 只使用提交绑定升级当前条目，不触发查询或文本去重。
func (m *Model) upgradeReasoning(sample protocol.SampleCommitPayload, sequence uint64) {
	b := &m.browser
	for i := range m.reasoning.items {
		preview := &m.reasoning.items[i]
		if preview.key.sample != sample.SampleIndex {
			continue
		}
		for _, binding := range sample.Bindings {
			if binding.Block == preview.key.block {
				preview.cursor = protocol.TranscriptCursor{Sequence: sequence, Item: uint64(binding.Item)}
				break
			}
		}
	}
	for i := range b.items {
		item := &b.items[i]
		if !item.temporary.live || item.temporary.sample != sample.SampleIndex {
			continue
		}
		for _, binding := range sample.Bindings {
			if item.temporary.block != binding.Block {
				continue
			}
			item.Cursor = protocol.TranscriptCursor{Sequence: sequence, Item: uint64(binding.Item)}
			total := uint64(0)
			if preview := m.reasoning.find(item.temporary); preview != nil {
				total = preview.total
			}
			item.Reasoning = &protocol.ReasoningSummaryPayload{Source: binding.Source, Availability: binding.Availability, TotalBytes: total}
			item.temporary = reasoningKey{}
			break
		}
	}
}

// reasoningPreviewForItem 将失败快照按真实 cursor 或临时键查找，不匹配正文。
func (m *Model) reasoningPreviewForItem(item transcriptDetailItem) *reasoningPreview {
	if item.temporary.live {
		return m.reasoning.find(item.temporary)
	}
	if item.Cursor.Valid() {
		for i := range m.reasoning.items {
			if m.reasoning.items[i].cursor == item.Cursor {
				return &m.reasoning.items[i]
			}
		}
	}
	return nil
}
func reasoningSourceLabel(source domain.ReasoningSource) string {
	switch source {
	case domain.ReasoningAnthropicThinking:
		return "Anthropic thinking"
	case domain.ReasoningOpenAISummary:
		return "OpenAI summary"
	default:
		return "Reasoning"
	}
}
