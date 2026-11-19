package tui

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

// mainTranscript 持有最新五十个逻辑项；未提交增量与查询快照分开拥有。
type mainTranscript struct {
	initialReady       bool
	hydrationEpoch     uint64
	awaitingProjection bool
	hydrating          bool
	hydrationCancel    context.CancelFunc
	attempted          map[protocol.TranscriptCursor]hydrationState
	handoff            []string
	generation         uint64
	sequence           uint64
	cancel             context.CancelFunc
	items              []transcriptMessage
}
type mainTranscriptReply struct {
	generation uint64
	page       protocol.TranscriptListPage
	err        error
}

func (model Model) initialTranscriptQuery() tea.Cmd {
	if model.main.initialReady {
		return nil
	}
	return model.queryMainTranscript(model.main.generation, 0, model.submitContext)
}

// LoadInitialTranscript 显式取得目标首个有界主线页，失败时不得提供可接管的模型。
func (model Model) LoadInitialTranscript(ctx context.Context) (Model, error) {
	thread := model.browser.thread
	if !thread.Valid() || model.tasks.threadID != thread || !model.tasks.sessionID.Valid() || model.browser.queries == nil || model.outputs == nil {
		return Model{}, fmt.Errorf("resumed conversation is not ready")
	}
	page, err := model.browser.queries.ListTranscript(ctx, mainTranscriptQuery(thread, 0))
	if err != nil || page.Validate() != nil {
		return Model{}, fmt.Errorf("initial conversation history is unavailable")
	}
	for _, item := range page.Items {
		if item.Kind == domain.TranscriptReasoning {
			return Model{}, fmt.Errorf("initial conversation history is invalid")
		}
	}
	model = model.applyMainTranscript(page)
	model.main.initialReady = true
	model.main.awaitingProjection = false
	model.prepareMarkdownView()
	return model, nil
}

func mainTranscriptQuery(thread domain.ThreadID, sequence uint64) protocol.TranscriptListQuery {
	return protocol.TranscriptListQuery{Mode: protocol.TranscriptMain, ThreadID: thread, Through: sequence, Limit: 50}
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
		page, err := queries.ListTranscript(ctx, mainTranscriptQuery(thread, sequence))
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
	model.main.resetHydration()
	model.main.awaitingProjection = false
	model.main.sequence = sequence
	model.main.generation++
	ctx := model.submitContext
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, model.main.cancel = context.WithCancel(ctx)
	start := max(0, len(model.markdown.lines)-model.transcriptHeight())
	if model.reading.detached {
		start = min(model.reading.scroll, start)
	}
	model.main.handoff = append([]string(nil), model.markdown.lines[start:min(len(model.markdown.lines), start+model.transcriptHeight())]...)
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
	if model.main.cancel != nil {
		model.main.cancel()
		model.main.cancel = nil
	}
	if reply.err != nil || reply.page.Validate() != nil {
		model.errorSummary = "History refresh failed"
		return model, nil
	}
	for _, item := range reply.page.Items {
		if model.main.sequence > 0 && item.Cursor.Sequence > model.main.sequence {
			model.errorSummary = "History refresh failed"
			return model, nil
		}
	}
	return model.applyMainTranscript(reply.page), nil
}

func (model Model) applyMainTranscript(page protocol.TranscriptListPage) Model {
	messages := make([]transcriptMessage, 0, len(page.Items))
	for i := len(page.Items) - 1; i >= 0; i-- {
		item := page.Items[i]
		message := transcriptMessage{text: item.Text, cursor: item.Cursor, more: item.More}
		switch item.Kind {
		case domain.TranscriptCompaction:
			message.role = "Compaction"
		case domain.TranscriptUser:
			message.role = "User"
			message.text += fileSummaryText(item.Files)
		case domain.TranscriptHook:
			message.role = "Hook"
		case domain.TranscriptAssistant:
			message.role = "Assistant"
		case domain.TranscriptCall:
			message.admitted = item.ExecutionAdmitted
			activity := item.Activity
			message.activity = &activity
			if item.PreHook != nil {
				summary := *item.PreHook
				message.hook = &summary
			}
			if item.PostHook != nil {
				summary := *item.PostHook
				message.hook = &summary
			}
			message.preview = item.Preview
			message.saving = savingNotice(item.Stdout, item.Stderr, item.Search)
		}
		messages = append(messages, message)
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].role == "Compaction" {
			messages = messages[i:]
			break
		}
	}
	through := model.main.sequence
	if through == 0 && len(page.Items) > 0 {
		through = page.Items[0].Cursor.Sequence
	}
	model.browser.mainListKey = transcriptPageKey{mode: protocol.TranscriptMain, kind: transcriptListPage, through: through}
	model.browser.mainListPinned = true
	model.browser.remember(cachedTranscriptPage{key: model.browser.mainListKey, list: page})
	model.main.items = messages
	model.main.awaitingProjection = true
	return model
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
	return messages[max(0, len(messages)-50):]
}

// appendProvisionalText 限制尚未 durable 的显示缓存，完整正文在提交后由范围查询提供。
func appendProvisionalText(current, delta string) string {
	const limit = 64 << 10
	text := current + delta
	if len(text) <= limit {
		return text
	}
	start := len(text) - limit
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	return strings.Clone(text[start:])
}

func (model Model) quitTranscript() (tea.Model, tea.Cmd) {
	model.closeFileQueries()
	model.browser.invalidate()
	model.main.resetHydration()
	if model.main.cancel != nil {
		model.main.cancel()
		model.main.cancel = nil
	}
	model.reasoning = activeReasoning{}
	model.browser.page = nil
	model.browser.items = nil
	model.browser.markdown = transcriptMarkdownDetail{}
	model.browser.queries = nil
	model.main.awaitingProjection = false
	model.main.handoff = nil
	model.quitRequested = true
	return model, tea.Quit
}
