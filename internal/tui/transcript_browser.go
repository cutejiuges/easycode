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
	backward bool
	kind     transcriptPageKind
	cursor   protocol.TranscriptCursor
	source   protocol.TranscriptBodySource
	stream   protocol.ToolOutputSource
	offset   uint64
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
	positions          [6]transcriptDetailPosition
	queries            protocol.TranscriptQueries
	thread             domain.ThreadID
	generation         uint64
	cancel             context.CancelFunc
	items              []protocol.TranscriptListItem
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
		b.cache = append([]cachedTranscriptPage(nil), b.cache[1:]...)
	}
	b.cache = append(b.cache, page)
}

func (model Model) openTranscriptBrowser() (tea.Model, tea.Cmd) {
	model.browser.invalidate()
	model.detailOpen = !model.detailOpen
	model.detailScroll = 0
	if !model.detailOpen {
		model.browser.page = nil
		model.browser.cache = nil
		model.browser.items = nil
		return model, nil
	}
	model.browser.detail = false
	return model.loadTranscriptPage(transcriptPageKey{kind: transcriptListPage})
}

func (model Model) loadTranscriptPage(key transcriptPageKey) (tea.Model, tea.Cmd) {
	b := &model.browser
	b.invalidate()
	b.page = nil
	model.detailScroll = 0
	if b.queries == nil {
		b.errorText = "Transcript unavailable"
		return model, nil
	}
	if key.kind == transcriptListPage {
		b.before = key.cursor
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
		selected = b.items[b.selected]
		invocation = selected.Activity.InvocationID
	}
	return model, func() tea.Msg {
		defer cancel()
		reply := transcriptReply{generation: generation, page: cachedTranscriptPage{key: key}}
		switch key.kind {
		case transcriptListPage:
			reply.page.list, reply.err = queries.ListTranscript(ctx, protocol.TranscriptListQuery{ThreadID: thread, Before: key.cursor, Newer: key.backward, Limit: 50})
		case transcriptBodyPage:
			reply.page.body, reply.err = queries.ReadTranscriptBody(ctx, protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: key.cursor, Source: key.source, Offset: key.offset, Backward: key.backward, Limit: 64 << 10})
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
			selected = b.items[b.selected].Cursor
		}
		b.items = append([]protocol.TranscriptListItem(nil), p.list.Items...)
		b.nextBefore = p.list.NextBefore
		b.selected = 0
		for i, item := range b.items {
			if item.Cursor == selected {
				b.selected = i
				break
			}
		}
	case transcriptBodyPage:
		if !b.detail || p.body.Validate() != nil || (!p.key.backward && p.body.Offset != p.key.offset) || (p.key.backward && p.body.NextOffset != p.key.offset) {
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
	return model, nil
}

func (model Model) refreshTranscriptBrowser() (Model, tea.Cmd) {
	b := &model.browser
	if b.detail {
		return model, nil
	}
	b.cache = nil
	updated, cmd := model.loadTranscriptPage(transcriptPageKey{kind: transcriptListPage, cursor: b.before})
	return updated.(Model), cmd
}

func (model Model) loadTranscriptDetail() (tea.Model, tea.Cmd) {
	b := &model.browser
	if b.selected < 0 || b.selected >= len(b.items) {
		return model, nil
	}
	key := transcriptPageKey{kind: transcriptBodyPage, cursor: b.items[b.selected].Cursor, source: b.source, offset: b.offset}
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
		return model, tea.Quit
	}
	if key.Type == tea.KeyEsc {
		b.invalidate()
		if b.detail {
			b.detail = false
			b.page = nil
		} else {
			model.detailOpen = false
			b.items = nil
			b.cache = nil
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
				return model.loadTranscriptPage(transcriptPageKey{kind: transcriptListPage, cursor: b.nextBefore})
			}
		case tea.KeyPgUp:
			if b.before.Valid() && len(b.items) > 0 {
				return model.loadTranscriptPage(transcriptPageKey{kind: transcriptListPage, cursor: b.items[0].Cursor, backward: true})
			}
		case tea.KeyEnter:
			if len(b.items) > 0 {
				item := b.items[b.selected]

				b.detail = true
				b.positions = [6]transcriptDetailPosition{}
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
			} else if item.Search.Readable {
				b.switchDetail(b.source, protocol.ToolSearchResult)
			} else if item.Stdout.Readable {
				b.switchDetail(b.source, protocol.ToolStdout)
			}
			return model.loadTranscriptDetail()
		}
	case tea.KeyLeft:
		if item.Kind == domain.TranscriptCall {
			if b.stream != "" {
				b.switchDetail(protocol.TranscriptResult, "")
			} else {
				b.switchDetail(protocol.TranscriptInput, "")
			}
			return model.loadTranscriptDetail()
		}
	case tea.KeyPgDown:
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
		if b.offset > 0 {
			if len(b.previous) == 0 {
				pageKey := transcriptPageKey{kind: transcriptBodyPage, cursor: item.Cursor, source: b.source, stream: b.stream, offset: b.offset, backward: true}
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
		model.detailScroll = max(0, model.detailScroll-1)
	case tea.KeyDown:
		model.detailScroll++
	}
	return model, nil
}

func (model Model) renderTranscriptBrowser() string {
	b := model.browser
	width, visible := max(8, model.width-4), max(1, model.height-8)
	if !b.detail {
		lines := []string{"Transcript"}
		start := max(0, b.selected-visible+1)
		for i := start; i < min(len(b.items), start+visible); i++ {
			item := b.items[i]
			marker := "  "
			if i == b.selected {
				marker = "> "
			}
			text := string(item.Kind) + ": " + item.Text
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
	lines := wrapApprovalDiff(commandDisplayText(body), width)

	headers := []string{title}
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
		headers = append(headers, b.errorText+" · ← returns to model result")
	}
	for i := range headers {
		headers[i] = truncateRunes(headers[i], width)
	}
	visible = max(1, model.heightOrDefault()-len(headers)-2)
	scroll := min(model.detailScroll, max(0, len(lines)-visible))
	return strings.Join(headers, "\n") + "\n" + strings.Join(lines[scroll:min(len(lines), scroll+visible)], "\n") + "\n" + truncateRunes("←/→: input/result/saved · PgUp/PgDn: page", width) + "\n" + truncateRunes("Esc: transcript · Ctrl+O: close", width) + "\n"
}
