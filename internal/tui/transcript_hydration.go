package tui

import (
	"context"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
)

type hydrationState uint8

const (
	hydrationUnrequested hydrationState = iota
	hydrationRequested
	hydrationFailed
	hydrationPaged
)

type transcriptHydration struct {
	epoch      uint64
	generation uint64
	cursor     protocol.TranscriptCursor
	page       protocol.TranscriptBodyPage
	err        error
}

func (model Model) assistantBody(message transcriptMessage) (string, bool) {
	if !message.more {
		return message.text, true
	}
	for _, page := range model.browser.cache {
		if page.key.kind == transcriptBodyPage && page.key.cursor == message.cursor && page.key.source == protocol.TranscriptText && page.body.Offset == 0 && page.body.NextOffset == page.body.TotalBytes {
			return page.body.Text, true
		}
	}
	return message.text, false
}
func (model *Model) scheduleHydration() tea.Cmd {
	if model.browser.queries == nil || model.main.hydrating || model.detailOpen {
		return nil
	}
	visible := make(map[protocol.TranscriptCursor]bool)
	start := max(0, len(model.reading.anchors)-model.transcriptHeight())
	if model.reading.detached {
		start = model.reading.scroll
	}
	for i := start; i < min(len(model.reading.anchors), start+model.transcriptHeight()); i++ {
		visible[model.reading.anchors[i].key.cursor] = true
	}
	if model.main.awaitingProjection {
		clear(visible)
		remaining := model.transcriptHeight()
		for i := len(model.main.items) - 1; i >= 0 && remaining > 0; i-- {
			item := model.main.items[i]
			visible[item.cursor] = true
			body, _ := model.assistantBody(item)
			for _, line := range strings.Split(body, "\n") {
				remaining -= max(1, (len(line)+max(1, model.widthOrDefault()-2)-1)/max(1, model.widthOrDefault()-2))
			}
		}
		if model.reading.detached {
			visible[model.reading.anchor.key.cursor] = true
		}
	}
	candidate := protocol.TranscriptCursor{}
	for i := len(model.main.items) - 1; i >= 0; i-- {
		item := model.main.items[i]
		if item.role != "Assistant" || !item.more || !visible[item.cursor] || model.main.attempted[item.cursor] != hydrationUnrequested {
			continue
		}
		if _, complete := model.assistantBody(item); complete {
			continue
		}
		if !candidate.Valid() {
			candidate = item.cursor
		}
		if model.reading.anchor.key.cursor == item.cursor {
			candidate = item.cursor
			break
		}
	}
	if !candidate.Valid() {
		return nil
	}
	// 四页已满时仅回收离屏且未被详情持有的页，不在同一窗口来回加载。
	if len(model.browser.cache) >= 4 {
		victim := -1
		for i, page := range model.browser.cache {
			if model.browser.protected(page.key) || visible[page.key.cursor] {
				continue
			}
			if model.browser.page != nil && model.browser.page.key == page.key {
				continue
			}
			victim = i
			break
		}
		if victim < 0 {
			return nil
		}
		model.browser.cache = append(model.browser.cache[:victim:victim], model.browser.cache[victim+1:]...)
	}
	if model.main.attempted == nil {
		model.main.attempted = make(map[protocol.TranscriptCursor]hydrationState)
	}
	model.main.attempted[candidate] = hydrationRequested
	model.main.hydrating = true
	ctx := model.submitContext
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	model.main.hydrationCancel = cancel
	epoch := model.main.hydrationEpoch
	generation, queries, thread := model.main.generation, model.browser.queries, model.browser.thread
	return func() tea.Msg {
		defer cancel()
		page, err := queries.ReadTranscriptBody(ctx, protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: candidate, Source: protocol.TranscriptText, Limit: 64 << 10})
		return transcriptHydration{epoch, generation, candidate, page, err}
	}
}
func (model Model) receiveHydration(reply transcriptHydration) (tea.Model, tea.Cmd) {
	if reply.generation != model.main.generation || reply.epoch != model.main.hydrationEpoch || !model.main.hydrating {
		return model, nil
	}
	model.main.hydrating = false
	model.main.hydrationCancel = nil
	if reply.err != nil || reply.page.Validate() != nil || reply.page.Offset != 0 {
		model.main.attempted[reply.cursor] = hydrationFailed
		return model, nil
	}
	if reply.page.NextOffset < reply.page.TotalBytes {
		model.main.attempted[reply.cursor] = hydrationPaged
		return model, nil
	}
	model.browser.remember(cachedTranscriptPage{key: transcriptPageKey{kind: transcriptBodyPage, cursor: reply.cursor, source: protocol.TranscriptText}, body: reply.page})
	return model, nil
}

func (main *mainTranscript) resetHydration() {
	if main.hydrationCancel != nil {
		main.hydrationCancel()
		main.hydrationCancel = nil
	}
	main.hydrationEpoch++
	main.attempted = nil
	main.hydrating = false
}
