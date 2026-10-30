package tui

import (
	"context"
	"strings"
	"unicode/utf8"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

// mainTranscript 持有最新五十个逻辑项；未提交增量与查询快照分开拥有。
type mainTranscript struct {
	generation uint64
	sequence   uint64
	cancel     context.CancelFunc
	items      []transcriptMessage
}
type mainTranscriptReply struct {
	generation uint64
	page       protocol.TranscriptListPage
	err        error
}

func (model Model) initialTranscriptQuery() tea.Cmd {
	return model.queryMainTranscript(model.main.generation, 0, model.submitContext)
}
func (model Model) queryMainTranscript(generation, sequence uint64, ctx context.Context) tea.Cmd {
	queries, thread := model.browser.queries, model.browser.thread
	if queries == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return func() tea.Msg {
		page, err := queries.ListTranscript(ctx, protocol.TranscriptListQuery{ThreadID: thread, Through: sequence, Limit: 50})
		return mainTranscriptReply{generation: generation, page: page, err: err}
	}
}
func (model Model) commitMainTranscript(sequence uint64) (tea.Model, tea.Cmd) {
	if model.browser.queries == nil {
		return model, nil
	}
	if sequence <= model.main.sequence {
		model.errorSummary = "stream_protocol_error: non-monotonic transcript commit"
		return model, nil
	}
	if model.main.cancel != nil {
		model.main.cancel()
	}
	model.main.sequence = sequence
	model.main.generation++
	ctx := model.submitContext
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, model.main.cancel = context.WithCancel(ctx)
	model.transcript = nil
	model.assistantItem = -1
	model.toolItems = nil
	model.toolClosed = nil
	model.toolActivities = nil
	return model, model.queryMainTranscript(model.main.generation, sequence, ctx)
}
func (model Model) receiveMainTranscript(reply mainTranscriptReply) (tea.Model, tea.Cmd) {
	if reply.generation != model.main.generation {
		return model, nil
	}
	if reply.err != nil || reply.page.Validate() != nil {
		model.errorSummary = "transcript_unavailable: committed history unavailable"
		return model, nil
	}
	messages := make([]transcriptMessage, 0, len(reply.page.Items))
	for i := len(reply.page.Items) - 1; i >= 0; i-- {
		item := reply.page.Items[i]
		message := transcriptMessage{text: item.Text}
		switch item.Kind {
		case domain.TranscriptUser:
			message.role = "User"
		case domain.TranscriptAssistant:
			message.role = "Assistant"
		case domain.TranscriptCall:
			activity := item.Activity
			message.activity = &activity
			message.preview = item.Preview
			message.saving = savingNotice(item.Stdout, item.Stderr, item.Search)
		}
		if item.More {
			message.text += " … [Ctrl+O for full text]"
		}
		messages = append(messages, message)
	}
	model.main.items = messages
	return model, nil
}
func (model Model) visibleTranscript() []transcriptMessage {
	messages := append([]transcriptMessage(nil), model.main.items...)
	indices := make(map[string]int)
	for i, message := range messages {
		if message.activity != nil {
			indices[message.activity.InvocationID] = i
		}
	}
	for _, message := range model.transcript {
		if message.activity != nil {
			if i, ok := indices[message.activity.InvocationID]; ok {
				messages[i] = message
				continue
			}
		}
		messages = append(messages, message)
	}
	return messages
}

// appendProvisionalText 限制尚未 durable 的显示缓存，完整正文在提交后由范围查询提供。
func appendProvisionalText(current, delta string) string {
	const limit = 64 << 10
	text := current + delta
	if len(text) <= limit {
		return text
	}
	start := len(text) - limit + 64
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	return "[Streaming excerpt; full text available after commit]\n" + strings.Clone(text[start:])
}
