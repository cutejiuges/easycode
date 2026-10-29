package tui

import (
	"context"
	"fmt"
	"strings"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type commandListReply struct {
	generation uint64
	page       protocol.CommandListPage
	err        error
}
type commandPageReply struct {
	generation uint64
	key        commandPageKey
	page       protocol.CommandOutputPage
	err        error
}
type commandPageKey struct {
	invocation string
	stream     protocol.CommandStream
	offset     uint64
}
type cachedCommandPage struct {
	key  commandPageKey
	page protocol.CommandOutputPage
}

// commandBrowser 持有至多五十条列表投影与四个已校验页；取消函数不持有外部资源所有权。
type commandBrowser struct {
	queries    protocol.CommandQueries
	thread     domain.ThreadID
	generation uint64
	cancel     context.CancelFunc
	items      []protocol.CommandHistoryItem
	selected   int
	nextBefore uint64
	before     uint64
	detail     bool
	stream     protocol.CommandStream
	offsets    [2][]uint64
	page       *protocol.CommandOutputPage
	cache      []cachedCommandPage
	loading    bool
	errorText  string
}

// WithCommandQueries 纯装配当前线程的宿主查询端口，不读取历史或文件。
func (model Model) WithCommandQueries(thread domain.ThreadID, queries protocol.CommandQueries) Model {
	model.browser.invalidate()
	model.browser = commandBrowser{thread: thread, queries: queries, stream: protocol.CommandStdout, generation: model.browser.generation}
	model.detailOpen = false
	return model
}

func (b *commandBrowser) invalidate() {
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	b.generation++
	b.loading = false
	b.errorText = ""
}

func (model Model) openCommandBrowser() (tea.Model, tea.Cmd) {
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
	return model.loadCommandList(0)
}

func (model Model) loadCommandList(before uint64) (tea.Model, tea.Cmd) {
	b := &model.browser
	b.invalidate()
	if b.queries == nil {
		b.errorText = "Command history unavailable"
		return model, nil
	}
	b.loading = true
	b.before = before
	b.items = nil
	b.selected = 0
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	generation, queries := b.generation, b.queries
	query := protocol.CommandListQuery{ThreadID: b.thread, Before: before, Limit: 50}
	return model, func() tea.Msg {
		defer cancel()
		page, err := queries.ListCommands(ctx, query)
		return commandListReply{generation, page, err}
	}
}

func (model Model) loadCommandPage() (tea.Model, tea.Cmd) {
	b := &model.browser
	b.invalidate()
	b.page = nil
	model.detailScroll = 0
	if b.selected < 0 || b.selected >= len(b.items) {
		return model, nil
	}
	item := b.items[b.selected]
	summary := item.Stdout
	streamIndex := 0
	if b.stream == protocol.CommandStderr {
		summary = item.Stderr
		streamIndex = 1
	}
	if !summary.Readable {
		return model, nil
	}
	if len(b.offsets[streamIndex]) == 0 {
		b.offsets[streamIndex] = []uint64{0}
	}
	offset := b.offsets[streamIndex][len(b.offsets[streamIndex])-1]
	key := commandPageKey{item.InvocationID, b.stream, offset}
	for _, cached := range b.cache {
		if cached.key == key {
			page := cached.page
			b.page = &page
			return model, nil
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	b.loading = true
	generation, queries := b.generation, b.queries
	query := protocol.CommandOutputQuery{ThreadID: b.thread, InvocationID: item.InvocationID, Stream: b.stream, Offset: offset, Limit: 65536}
	return model, func() tea.Msg {
		defer cancel()
		page, err := queries.ReadCommandOutput(ctx, query)
		return commandPageReply{generation, key, page, err}
	}
}

func (model Model) receiveCommandList(reply commandListReply) (tea.Model, tea.Cmd) {
	b := &model.browser
	if !model.detailOpen || reply.generation != b.generation {
		return model, nil
	}
	b.loading = false
	if reply.err != nil {
		b.errorText = "Command history unavailable"
		return model, nil
	}
	if len(reply.page.Items) > 50 {
		b.errorText = "Invalid command history page"
		return model, nil
	}
	selected := ""
	if b.selected < len(b.items) {
		selected = b.items[b.selected].InvocationID
	}
	b.items = append([]protocol.CommandHistoryItem(nil), reply.page.Items...)
	b.nextBefore = reply.page.NextBefore
	for i, item := range b.items {
		if item.InvocationID == selected {
			b.selected = i
			break
		}
	}
	if b.detail {
		return model.loadCommandPage()
	}
	return model, nil
}

func (model Model) refreshCommandBrowser() (Model, tea.Cmd) {
	b := &model.browser
	b.invalidate()
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	generation, queries := b.generation, b.queries
	query := protocol.CommandListQuery{ThreadID: b.thread, Before: b.before, Limit: 50}
	return model, func() tea.Msg {
		defer cancel()
		page, err := queries.ListCommands(ctx, query)
		return commandListReply{generation, page, err}
	}
}

func (model Model) receiveCommandPage(reply commandPageReply) (tea.Model, tea.Cmd) {
	b := &model.browser
	if !model.detailOpen || !b.detail || reply.generation != b.generation {
		return model, nil
	}
	b.loading = false
	if reply.err != nil {
		b.errorText = "Saved output unavailable: " + commandDisplayText(reply.err.Error())
		return model, nil
	}
	if len(reply.page.Text) > 256<<10 || reply.page.Offset != reply.key.offset || reply.page.NextOffset < reply.page.Offset || reply.page.NextOffset-reply.page.Offset > 65536 {
		b.errorText = "Invalid saved output page"
		return model, nil
	}
	page := reply.page
	b.page = &page
	if len(b.cache) == 4 {
		b.cache = append([]cachedCommandPage(nil), b.cache[1:]...)
	}
	b.cache = append(b.cache, cachedCommandPage{reply.key, page})
	return model, nil
}

func (model Model) handleCommandBrowserKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	b := &model.browser
	if key.Type == tea.KeyCtrlC && model.state != stateIdle {
		model.cancelling = true
		if model.events == nil {
			model.interruptNext = true
			return model, nil
		}
		return model, interruptSession(model.session)
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
			if b.nextBefore != 0 {
				return model.loadCommandList(b.nextBefore)
			}
		case tea.KeyPgUp:
			if b.before != 0 {
				return model.loadCommandList(0)
			}
		case tea.KeyEnter:
			if len(b.items) > 0 {
				b.detail = true
				b.stream = protocol.CommandStdout
				b.offsets = [2][]uint64{}
				return model.loadCommandPage()
			}
		}
		return model, nil
	}
	index := 0
	if b.stream == protocol.CommandStderr {
		index = 1
	}
	switch key.Type {
	case tea.KeyTab:
		if index == 0 {
			b.stream = protocol.CommandStderr
		} else {
			b.stream = protocol.CommandStdout
		}
		return model.loadCommandPage()
	case tea.KeyPgDown:
		if b.page != nil && !b.page.StoredEOF {
			b.offsets[index] = append(append([]uint64(nil), b.offsets[index]...), b.page.NextOffset)
			return model.loadCommandPage()
		}
	case tea.KeyPgUp:
		if len(b.offsets[index]) > 1 {
			b.offsets[index] = b.offsets[index][:len(b.offsets[index])-1]
			return model.loadCommandPage()
		}
	case tea.KeyUp:
		model.detailScroll = max(0, model.detailScroll-1)
	case tea.KeyDown:
		model.detailScroll++
	}
	return model, nil
}

func (model Model) renderCommandBrowser() string {
	b := model.browser
	width, visible := max(16, model.width-4), max(1, model.height-7)
	if !b.detail {
		lines := []string{"Command history"}
		start := max(0, b.selected-visible+1)
		for i := start; i < min(len(b.items), start+visible); i++ {
			marker := "  "
			if i == b.selected {
				marker = "> "
			}
			status := b.items[i].Status
			if live, exists := model.commandViews[b.items[i].InvocationID]; exists && !b.items[i].Terminal {
				status = live.activity.Status
			}
			lines = append(lines, truncateRunes(marker+status+" · "+commandDisplayText(b.items[i].Command), width))
		}
		if b.loading {
			lines = append(lines, "Loading…")
		} else if len(b.items) == 0 {
			lines = append(lines, "No command history")
		}
		if b.errorText != "" {
			lines = append(lines, b.errorText)
		}
		return strings.Join(lines, "\n") + "\n↑/↓: select · Enter: details · PgDn: older\nCtrl+O/Esc: close\n"
	}
	if b.selected >= len(b.items) {
		return "Command details unavailable\n"
	}
	item := b.items[b.selected]
	summary := item.Stdout
	if b.stream == protocol.CommandStderr {
		summary = item.Stderr
	}
	body := "Command:\n" + item.Command + "\n\nFrozen preview:\n" + item.Preview
	if !item.Terminal {
		if live, exists := model.commandViews[item.InvocationID]; exists {
			body = "Command:\n" + item.Command + "\n\nRecent output:\n" + live.activity.Recent
		}
	}
	if b.page != nil {
		ending := ""
		if b.page.StoredEOF {
			ending = " · end of saved output"
			if !b.page.SourceComplete {
				ending += " (command output incomplete)"
			}
		}
		body = "Command:\n" + item.Command + fmt.Sprintf("\n\n%s bytes %d-%d of %d%s:\n", b.stream, b.page.Offset, b.page.NextOffset, b.page.StoredBytes, ending) + b.page.Text + "\n\nFrozen preview:\n" + item.Preview
	}
	if b.loading {
		body += "\nLoading saved output…"
	}
	if b.errorText != "" {
		body += "\n" + b.errorText
	}
	lines := wrapApprovalDiff(commandDisplayText(body), width)
	scroll := min(model.detailScroll, max(0, len(lines)-visible))
	state := item.Status
	if live, exists := model.commandViews[item.InvocationID]; exists && !item.Terminal {
		state = live.activity.Status
	}
	if item.ExitKnown {
		state += fmt.Sprintf(" · exit=%d", item.ExitCode)
	}
	if model.cancelling && !item.Terminal {
		state = "Cancelling — waiting for cleanup"
	}
	workdir := item.Workdir
	if workdir == "" {
		workdir = "."
	}
	storage := "Saving while command runs"
	switch summary.Status {
	case "complete":
		storage = fmt.Sprintf("Saved complete: %d bytes", summary.SavedBytes)
	case "partial":
		storage = fmt.Sprintf("Saved prefix: %d of %d bytes", summary.SavedBytes, summary.ObservedBytes)
	case "unavailable":
		storage = "Saved output unavailable: " + summary.Reason
	case "not_needed":
		storage = "Output included in preview"
	}
	if item.Truncated {
		storage += " · preview truncated"
	}
	headers := []string{"Command details · " + string(b.stream), state + fmt.Sprintf(" · %d ms · %s", item.DurationMillis, commandDisplayText(workdir)), storage}
	for i := range headers {
		headers[i] = truncateRunes(headers[i], width)
	}
	return strings.Join(headers, "\n") + "\n" + strings.Join(lines[scroll:min(len(lines), scroll+visible)], "\n") + "\n" + truncateRunes("Tab: stream · PgUp/PgDn: page · ↑/↓: scroll", width) + "\n" + truncateRunes("Esc: list · Ctrl+O: close · Ctrl+C: interrupt", width) + "\n"
}
