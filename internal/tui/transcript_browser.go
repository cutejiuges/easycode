package tui

import (
	"context"
	"fmt"
	"strings"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type transcriptPageKind uint8

const (
	transcriptListPage transcriptPageKind = iota
	transcriptBodyPage
	transcriptOutputPage
)

type transcriptPageKey struct {
	mode           protocol.TranscriptListMode
	detailSequence uint64
	through        uint64
	backward       bool
	kind           transcriptPageKind
	cursor         protocol.TranscriptCursor
	source         protocol.TranscriptBodySource
	stream         protocol.ToolOutputSource
	offset         uint64
}
type cachedTranscriptPage struct {
	key    transcriptPageKey
	list   protocol.TranscriptListPage
	body   protocol.TranscriptBodyPage
	output protocol.ToolOutputPage
}
type transcriptReply struct {
	extras           []cachedTranscriptPage
	savedUnavailable bool
	generation       uint64
	page             cachedTranscriptPage
	err              error
}

// transcriptBrowser 只保存四个查询页，草稿、主线滚动与查询生命周期分别持有。
type transcriptDetailPosition struct {
	offset   uint64
	previous []uint64
}

type transcriptBrowser struct {
	preferLive         bool
	mainListKey        transcriptPageKey
	mainListPinned     bool
	listKey            transcriptPageKey
	listPinned         bool
	entryCursor        protocol.TranscriptCursor
	markdown           transcriptMarkdownDetail
	anchorCursor       protocol.TranscriptCursor
	visibleCursors     []protocol.TranscriptCursor
	positions          [7]transcriptDetailPosition
	queries            protocol.TranscriptQueries
	thread             domain.ThreadID
	generation         uint64
	cancel             context.CancelFunc
	items              []transcriptDetailItem
	selected           int
	before, nextBefore protocol.TranscriptCursor
	detail             bool
	source             protocol.TranscriptBodySource
	stream             protocol.ToolOutputSource
	offset             uint64
	previous           []uint64
	page               *cachedTranscriptPage
	cache              []cachedTranscriptPage
	loading            bool
	errorText          string
}

// WithTranscriptQueries 只装配宿主端口，首个查询通过 Init 或按键命令显式执行。
func (model Model) WithTranscriptQueries(thread domain.ThreadID, queries protocol.TranscriptQueries) Model {
	model.browser.invalidate()
	color := model.markdown.color
	model.markdown = newMarkdownViewCache()
	model.markdown.color = color
	if model.main.hydrationCancel != nil {
		model.main.hydrationCancel()
	}
	model.browser = transcriptBrowser{thread: thread, queries: queries, generation: model.browser.generation}
	model.detailOpen = false
	if model.main.cancel != nil {
		model.main.cancel()
	}
	model.main = mainTranscript{generation: model.main.generation + 1}
	return model
}

func (b *transcriptBrowser) invalidate() {
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	b.generation++
	b.loading = false
	b.errorText = ""
}
func (b *transcriptBrowser) remember(page cachedTranscriptPage) {
	for i, cached := range b.cache {
		if cached.key == page.key {
			b.cache = append(b.cache[:i:i], b.cache[i+1:]...)
			break
		}
	}
	if len(b.cache) == 4 {
		victim := -1
		for pass := 0; pass < 2 && victim < 0; pass++ {
			for i, cached := range b.cache {
				if b.protected(cached.key) {
					continue
				}
				visible := false
				for _, cursor := range b.visibleCursors {
					if cursor == cached.key.cursor {
						visible = true
						break
					}
				}
				if pass == 0 && visible {
					continue
				}
				victim = i
				break
			}
		}
		if victim < 0 {
			return
		}
		b.cache = append(b.cache[:victim:victim], b.cache[victim+1:]...)
	}
	b.cache = append(b.cache, page)
}

func (model Model) openTranscriptBrowser() (tea.Model, tea.Cmd) {
	model.browser.invalidate()
	model.detailOpen = !model.detailOpen
	model.browser.listPinned = model.detailOpen
	model.main.resetHydration()
	model.detailScroll = 0
	if !model.detailOpen {
		model.browser.page = nil
		model.browser.markdown = transcriptMarkdownDetail{}
		model.browser.items = nil
		return model, nil
	}
	model.browser.detail = false
	model.browser.preferLive = true
	model.browser.entryCursor = protocol.TranscriptCursor{}
	if model.reading.detached {
		model.browser.entryCursor = model.reading.anchor.key.cursor
	}
	through := model.main.sequence
	if through == 0 && model.browser.mainListPinned {
		through = model.browser.mainListKey.through
	}
	return model.loadTranscriptPage(transcriptPageKey{mode: protocol.TranscriptDetail, kind: transcriptListPage, through: through})
}

func (model Model) loadTranscriptPage(key transcriptPageKey) (tea.Model, tea.Cmd) {
	b := &model.browser
	b.invalidate()
	b.page = nil
	b.markdown = transcriptMarkdownDetail{}
	model.detailScroll = 0
	if b.queries == nil {
		b.errorText = "Transcript unavailable"
		return model, nil
	}
	if key.kind == transcriptListPage {
		b.before = key.cursor
		b.listKey = key
		b.detail = false
		b.selected = 0
	}
	for _, cached := range b.cache {
		if cached.key == key {
			return model.receiveTranscriptPage(transcriptReply{generation: b.generation, page: cached})
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	b.loading = true
	generation, queries, thread := b.generation, b.queries, b.thread
	invocation := ""
	var selected protocol.TranscriptListItem
	if b.selected < len(b.items) {
		selected = b.items[b.selected].TranscriptListItem
		invocation = selected.Activity.InvocationID
	}
	return model, func() tea.Msg {
		defer cancel()
		reply := transcriptReply{generation: generation, page: cachedTranscriptPage{key: key}}
		switch key.kind {
		case transcriptListPage:
			reply.page.list, reply.err = queries.ListTranscript(ctx, protocol.TranscriptListQuery{Mode: key.mode, ThreadID: thread, Before: key.cursor, Newer: key.backward, Through: key.through, Limit: 50})
		case transcriptBodyPage:
			reply.page.body, reply.err = queries.ReadTranscriptBody(ctx, protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: key.cursor, Source: key.source, DetailSequence: key.detailSequence, Offset: key.offset, Backward: key.backward, Limit: 64 << 10})
		case transcriptOutputPage:
			reply.page.output, reply.err = queries.ReadToolOutput(ctx, protocol.ToolOutputQuery{ThreadID: thread, InvocationID: invocation, Stream: key.stream, Offset: key.offset, Backward: key.backward, Limit: 64 << 10})
		}
		if reply.err == nil && key.kind == transcriptBodyPage && key.source == protocol.TranscriptResult && key.offset == 0 && selected.Activity.Tool == protocol.ToolShell {
			inputKey := transcriptPageKey{kind: transcriptBodyPage, cursor: key.cursor, source: protocol.TranscriptInput}
			input, err := queries.ReadTranscriptBody(ctx, protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: key.cursor, Source: protocol.TranscriptInput, Limit: 64 << 10})
			if err == nil && input.Validate() == nil {
				reply.extras = append(reply.extras, cachedTranscriptPage{key: inputKey, body: input})
			}
			stream := protocol.ToolOutputSource("")
			if selected.Stdout.Readable {
				stream = protocol.ToolStdout
			} else if selected.Stderr.Readable {
				stream = protocol.ToolStderr
			}
			if stream != "" {
				output, err := queries.ReadToolOutput(ctx, protocol.ToolOutputQuery{ThreadID: thread, InvocationID: invocation, Stream: stream, Limit: 64 << 10})
				if err == nil && output.Validate() == nil {
					reply.extras = append(reply.extras, cachedTranscriptPage{key: transcriptPageKey{kind: transcriptOutputPage, cursor: key.cursor, stream: stream, source: key.source}, output: output})
				} else {
					reply.savedUnavailable = true
				}
			}
		}
		return reply
	}
}

func (model Model) receiveTranscriptPage(reply transcriptReply) (tea.Model, tea.Cmd) {
	b := &model.browser
	if !model.detailOpen || reply.generation != b.generation {
		return model, nil
	}
	b.loading = false
	if reply.err != nil {
		b.errorText = "Content unavailable"
		if reply.page.key.kind == transcriptOutputPage {
			b.errorText = "Saved output unavailable"
		}
		return model, nil
	}
	p := reply.page
	switch p.key.kind {
	case transcriptListPage:
		if p.list.Validate() != nil {
			b.errorText = "Invalid transcript page"
			return model, nil
		}
		selected := protocol.TranscriptCursor{}
		if b.selected < len(b.items) {
			selected = b.items[b.selected].TranscriptListItem.Cursor
		}
		if b.entryCursor.Valid() {
			selected = b.entryCursor
			b.entryCursor = protocol.TranscriptCursor{}
		}
		b.items = detailItems(p.list.Items)
		b.nextBefore = p.list.NextBefore
		b.selected = 0
		for i, item := range b.items {
			if item.Cursor == selected {
				b.selected = i
				break
			}
		}
	case transcriptBodyPage:
		if !b.detail || p.body.Validate() != nil || p.body.DetailSequence != p.key.detailSequence || (!p.key.backward && p.body.Offset != p.key.offset) || (p.key.backward && p.body.NextOffset != p.key.offset) {
			b.errorText = "Invalid transcript body"
			return model, nil
		}
		b.offset = p.body.Offset
		b.page = &p
	case transcriptOutputPage:
		if !b.detail || p.output.Validate() != nil || (!p.key.backward && p.output.Offset != p.key.offset) || (p.key.backward && p.output.NextOffset != p.key.offset) {
			b.errorText = "Invalid saved output page"
			return model, nil
		}
		b.offset = p.output.Offset
		b.page = &p
	}
	for _, extra := range reply.extras {
		b.remember(extra)
	}
	if reply.savedUnavailable {
		b.errorText = "Saved output unavailable"
	}
	b.remember(p)
	model.mergeLiveReasoning()
	for _, extra := range reply.extras {
		found := false
		for _, cached := range b.cache {
			if cached.key == extra.key {
				found = true
				break
			}
		}
		if !found {
			b.errorText = "Some detail content collapsed: display cache budget"
		}
	}
	return model, nil
}

func (model Model) refreshTranscriptBrowser() (Model, tea.Cmd) {
	b := &model.browser
	if b.detail {
		return model, nil
	}
	key := transcriptPageKey{mode: protocol.TranscriptDetail, kind: transcriptListPage, cursor: b.before}
	for i, page := range b.cache {
		if page.key == key && page.key != b.mainListKey {
			b.cache = append(b.cache[:i:i], b.cache[i+1:]...)
			break
		}
	}
	updated, cmd := model.loadTranscriptPage(key)
	return updated.(Model), cmd
}

func (model Model) loadTranscriptDetail() (tea.Model, tea.Cmd) {
	b := &model.browser
	if b.selected < 0 || b.selected >= len(b.items) {
		return model, nil
	}
	if model.reasoningPreviewForItem(b.items[b.selected]) != nil {
		b.invalidate()
		model.prepareLiveReasoningPage()
		return model, nil
	}
	if b.items[b.selected].Reasoning != nil && b.items[b.selected].Reasoning.Availability == domain.ReasoningUnavailable {
		b.page = nil
		b.errorText = "Reasoning text unavailable"
		return model, nil
	}
	key := transcriptPageKey{kind: transcriptBodyPage, cursor: b.items[b.selected].Cursor, source: b.source, offset: b.offset}
	if b.source == protocol.TranscriptPostHooks {
		key.detailSequence = b.items[b.selected].DetailSequence
	}
	if b.stream != "" {
		key.kind = transcriptOutputPage
		key.stream = b.stream
	}
	return model.loadTranscriptPage(key)
}

func detailPositionIndex(source protocol.TranscriptBodySource, stream protocol.ToolOutputSource) int {
	switch stream {
	case protocol.ToolStdout:
		return 3
	case protocol.ToolStderr:
		return 4
	case protocol.ToolSearchResult:
		return 5
	}
	switch source {
	case protocol.TranscriptInput:
		return 1
	case protocol.TranscriptPostHooks:
		return 6
	case protocol.TranscriptResult:
		return 2
	default:
		return 0
	}
}
func (b *transcriptBrowser) switchDetail(source protocol.TranscriptBodySource, stream protocol.ToolOutputSource) {
	b.positions[detailPositionIndex(b.source, b.stream)] = transcriptDetailPosition{b.offset, append([]uint64(nil), b.previous...)}
	b.source, b.stream = source, stream
	position := b.positions[detailPositionIndex(source, stream)]
	b.offset, b.previous = position.offset, append([]uint64(nil), position.previous...)
}

func (model Model) handleTranscriptBrowserKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	b := &model.browser
	if key.Type == tea.KeyCtrlC {
		if model.activeTurnID.Valid() {
			model.cancelling = true
			return model, interruptSession(model.session)
		}
		if model.pending != nil {
			model.interruptNext = true
			return model, nil
		}
		if model.composer.text != "" {
			return model, nil
		}
		b.invalidate()
		return model.quitTranscript()
	}
	if key.Type == tea.KeyEsc {
		b.invalidate()
		if b.detail {
			b.detail = false
			b.page = nil
			b.markdown = transcriptMarkdownDetail{}
		} else {
			model.detailOpen = false
			b.listPinned = false
			b.items = nil
		}
		return model, nil
	}
	if !b.detail {
		switch key.Type {
		case tea.KeyUp:
			b.selected = max(0, b.selected-1)
		case tea.KeyDown:
			b.selected = min(max(0, len(b.items)-1), b.selected+1)
		case tea.KeyPgDown:
			if b.nextBefore.Valid() {
				return model.loadTranscriptPage(transcriptPageKey{mode: protocol.TranscriptDetail, kind: transcriptListPage, cursor: b.nextBefore, through: b.listKey.through})
			}
		case tea.KeyPgUp:
			if b.before.Valid() && len(b.items) > 0 {
				return model.loadTranscriptPage(transcriptPageKey{mode: protocol.TranscriptDetail, kind: transcriptListPage, cursor: b.items[0].Cursor, backward: true, through: b.listKey.through})
			}
		case tea.KeyEnter:
			if len(b.items) > 0 {
				item := b.items[b.selected]

				b.detail = true
				b.markdown = transcriptMarkdownDetail{}
				b.positions = [7]transcriptDetailPosition{}
				b.stream = ""
				b.offset = 0
				b.previous = nil
				b.source = protocol.TranscriptText
				if item.Kind == domain.TranscriptCall {
					b.source = protocol.TranscriptResult
					if item.Activity.Status == "waiting" || item.Activity.Status == "running" {
						b.source = protocol.TranscriptInput
					}
				}
				return model.loadTranscriptDetail()
			}
		}
		return model, nil
	}
	item := b.items[b.selected]
	if key.Type == tea.KeyRunes && string(key.Runes) == "r" && (item.Kind == domain.TranscriptAssistant || item.Kind == domain.TranscriptReasoning) && b.page != nil && b.page.body.Offset == 0 && b.page.body.NextOffset == b.page.body.TotalBytes {
		b.markdown.raw = !b.markdown.raw
		b.markdown.preserveAnchor = true
		return model, nil
	}
	switch key.Type {
	case tea.KeyTab:
		if item.Activity.Tool == protocol.ToolShell {
			stream := protocol.ToolStdout
			if b.stream == "" || b.stream == protocol.ToolStdout {
				stream = protocol.ToolStderr
			}
			b.switchDetail(b.source, stream)
			return model.loadTranscriptDetail()
		}
	case tea.KeyRight:
		if item.Kind == domain.TranscriptCall {
			if b.source == protocol.TranscriptInput && b.stream == "" {
				b.switchDetail(protocol.TranscriptResult, "")
			} else if b.source == protocol.TranscriptResult && b.stream == "" && item.DetailSequence > 0 {
				b.switchDetail(protocol.TranscriptPostHooks, "")
			} else if item.Search.Readable {
				b.switchDetail(b.source, protocol.ToolSearchResult)
			} else if item.Stdout.Readable {
				b.switchDetail(b.source, protocol.ToolStdout)
			}
			return model.loadTranscriptDetail()
		}
	case tea.KeyLeft:
		if item.Kind == domain.TranscriptCall {
			if b.stream != "" || b.source == protocol.TranscriptPostHooks {
				b.switchDetail(protocol.TranscriptResult, "")
			} else {
				b.switchDetail(protocol.TranscriptInput, "")
			}
			return model.loadTranscriptDetail()
		}
	case tea.KeyPgDown:
		if model.reasoningPreviewForItem(item) != nil {
			return model, nil
		}
		if b.page != nil {
			next, total := b.page.body.NextOffset, b.page.body.TotalBytes
			if b.stream != "" {
				next, total = b.page.output.NextOffset, b.page.output.StoredBytes
			}
			if next < total {
				if len(b.previous) == 4 {
					b.previous = b.previous[1:]
				}
				b.previous = append(b.previous, b.offset)
				b.offset = next
				return model.loadTranscriptDetail()
			}
		}
	case tea.KeyPgUp:
		if model.reasoningPreviewForItem(item) != nil {
			return model, nil
		}
		if b.offset > 0 {
			if len(b.previous) == 0 {
				pageKey := transcriptPageKey{kind: transcriptBodyPage, cursor: item.Cursor, source: b.source, stream: b.stream, offset: b.offset, backward: true}
				if b.source == protocol.TranscriptPostHooks {
					pageKey.detailSequence = item.DetailSequence
				}
				if b.stream != "" {
					pageKey.kind = transcriptOutputPage
				}
				return model.loadTranscriptPage(pageKey)
			}
			if len(b.previous) > 0 {
				b.offset = b.previous[len(b.previous)-1]
				b.previous = b.previous[:len(b.previous)-1]
			}
			return model.loadTranscriptDetail()
		}
	case tea.KeyUp:
		b.markdown.anchorKnown = false
		model.detailScroll = max(0, model.detailScroll-1)
	case tea.KeyDown:
		b.markdown.anchorKnown = false
		model.detailScroll++
	}
	return model, nil
}

func (model Model) renderTranscriptBrowser() string {
	b := model.browser
	width, visible := max(8, model.width-4), max(1, model.height-8)
	if !b.detail {
		lines := []string{"Transcript"}
		if model.cancelling {
			lines = append(lines, "Cancelling — waiting for cleanup")
		}
		start := max(0, b.selected-visible+1)
		for i := start; i < min(len(b.items), start+visible); i++ {
			item := b.items[i]
			marker := "  "
			if i == b.selected {
				marker = "> "
			}
			text := string(item.Kind) + ": " + item.Text
			if item.Kind == domain.TranscriptReasoning && item.Reasoning != nil {
				text = "Reasoning · " + reasoningSourceLabel(item.Reasoning.Source) + " · " + string(item.Reasoning.Availability)
				if item.temporary.live {
					text += " · live"
				}
			}
			if item.Kind == domain.TranscriptCall {
				text = string(item.Activity.Tool) + " · " + item.Activity.Status + " · " + item.Activity.Summary
			}
			lines = append(lines, truncateRunes(marker+commandDisplayText(strings.ReplaceAll(text, "\n", " ")), width))
		}
		if b.loading {
			lines = append(lines, "Loading…")
		} else if len(b.items) == 0 {
			lines = append(lines, "No conversation history")
		}
		if b.errorText != "" {
			lines = append(lines, b.errorText)
		}
		return strings.Join(lines, "\n") + "\n" + truncateRunes("↑/↓: select · Enter: details · PgUp/PgDn: page", width) + "\n" + truncateRunes("Ctrl+O/Esc: close", width) + "\n"
	}
	if b.selected >= len(b.items) {
		return "Details unavailable\n"
	}
	item := b.items[b.selected]
	title := string(item.Kind)
	if item.Kind == domain.TranscriptReasoning && item.Reasoning != nil {
		title = "Reasoning · " + reasoningSourceLabel(item.Reasoning.Source)
		if model.cancelling {
			title += " · Cancelling — waiting for cleanup"
		} else if model.reasoningPreviewForItem(item) != nil {
			title += " · " + model.reasoning.status
		}
	}
	if item.Kind == domain.TranscriptCall {
		title = string(item.Activity.Tool) + " · " + item.Activity.Status
		if item.Activity.ExitKnown {
			title += fmt.Sprintf(" · exit=%d", item.Activity.ExitCode)
		}
	}
	body := item.Preview
	if b.page != nil {
		body = b.page.body.Text
		if b.stream != "" {
			body = b.page.output.Text
		}
	}
	if item.Activity.Tool == protocol.ToolShell && b.stream == "" && b.source == protocol.TranscriptResult && b.offset == 0 {
		for _, cached := range b.cache {
			if cached.key.kind == transcriptBodyPage && cached.key.cursor == item.Cursor && cached.key.source == protocol.TranscriptInput && cached.key.offset == 0 {
				body = cached.body.Text + "\n\n" + body
				break
			}
		}
	}
	if item.Activity.Tool == protocol.ToolShell && b.stream == "" && b.source == protocol.TranscriptInput && item.Activity.Status == "running" {
		activity := item.Activity
		if live, ok := model.toolActivities[activity.InvocationID]; ok {
			activity = live.activity
		}
		if activity.Recent != "" {
			body += "\n\n" + activity.Recent
		}
	}
	var lines []string
	if (item.Kind == domain.TranscriptAssistant || item.Kind == domain.TranscriptReasoning) && b.page != nil {
		lines = make([]string, len(b.markdown.rows))
		for i, row := range b.markdown.rows {
			lines[i] = row.text
		}
		if b.page.body.Offset != 0 || b.page.body.NextOffset != b.page.body.TotalBytes {
			title += fmt.Sprintf(" · Raw excerpt %d–%d/%d", b.page.body.Offset, b.page.body.NextOffset, b.page.body.TotalBytes)
		} else if b.markdown.raw {
			title += " · Raw · r: Markdown"
		} else {
			title += " · Markdown · r: raw"
		}
	} else {
		lines = wrapApprovalDiff(commandDisplayText(body), width)
	}

	headers := []string{title}
	if b.markdown.notice != "" {
		headers = append(headers, b.markdown.notice)
	}
	if item.Activity.Tool == protocol.ToolShell {
		activity := item.Activity
		if live, ok := model.toolActivities[activity.InvocationID]; ok {
			activity = live.activity
		}
		workdir := activity.Workdir
		if workdir == "" {
			workdir = "."
		}
		headers = append(headers, "Workdir: "+commandDisplayText(workdir), fmt.Sprintf("Elapsed: %d ms", activity.ElapsedMillis), fmt.Sprintf("stdout: %d bytes; stderr: %d bytes", activity.StdoutBytes, activity.StderrBytes))
	}
	if item.ModelShortened {
		headers = append(headers, "Model saw a shortened result")
	}
	if item.CoverageIncomplete {
		headers = append(headers, "Search range was limited")
	}
	if item.SummaryOnly {
		headers = append(headers, "Historical change summary; no saved diff")
	}
	summary := item.Search
	if b.stream == protocol.ToolStdout {
		summary = item.Stdout
	} else if b.stream == protocol.ToolStderr {
		summary = item.Stderr
	}
	if summary.Readable {
		description := fmt.Sprintf("Saved %d bytes", summary.SavedBytes)
		if summary.Complete {
			description += " · complete"
		} else {
			description += " · partial prefix"
		}
		headers = append(headers, description)
	} else if summary.Status == "unavailable" {
		headers = append(headers, "Saved output unavailable")
	}
	if b.page != nil && b.stream != "" && b.page.output.StoredEOF {
		headers = append(headers, "End of saved content")
	}
	if b.loading {
		headers = append(headers, "Loading…")
	}
	if b.errorText != "" {
		if item.Kind == domain.TranscriptReasoning {
			headers = append(headers, b.errorText)
		} else {
			headers = append(headers, b.errorText+" · ← returns to model result")
		}
	}
	for i := range headers {
		headers[i] = truncateRunes(headers[i], width)
	}
	visible = max(1, model.heightOrDefault()-len(headers)-2)
	scroll := min(model.detailScroll-b.markdown.first, max(0, len(lines)-visible))
	return strings.Join(headers, "\n") + "\n" + strings.Join(lines[scroll:min(len(lines), scroll+visible)], "\n") + "\n" + truncateRunes("←/→: input/result/saved · PgUp/PgDn: page", width) + "\n" + truncateRunes("Esc: transcript · Ctrl+O: close", width) + "\n"
}

func (b *transcriptBrowser) protected(key transcriptPageKey) bool {
	return b.mainListPinned && key == b.mainListKey || b.listPinned && key == b.listKey || key.kind == transcriptBodyPage && key.source == protocol.TranscriptText && key.cursor == b.anchorCursor && b.anchorCursor.Valid() || b.page != nil && key == b.page.key
}
