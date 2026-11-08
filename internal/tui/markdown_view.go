package tui

import (
	"easycode/internal/protocol"
	"strings"
	"unicode/utf8"
	"unsafe"
)

type markdownViewKey struct {
	reasoning  reasoningKey
	cursor     protocol.TranscriptCursor
	generation uint64
	local      uint64
}

type markdownViewEntry struct {
	key    markdownViewKey
	source string
	stream markdownStream
	final  bool
	layout markdownLayout
	width  int
	color  bool
}

// markdownViewCache 在 Update 内准备展示，View 不改变解析器或缓存。
type markdownViewCache struct {
	externalBytes int
	parser        markdownParser
	entries       []markdownViewEntry
	color         bool
	lines         []string
}

func newMarkdownViewCache() markdownViewCache { return markdownViewCache{parser: newMarkdownParser()} }
func (c *markdownViewCache) prepare(key markdownViewKey, source string, width int, final bool) []string {
	index := -1
	for i := range c.entries {
		if c.entries[i].key == key {
			index = i
			break
		}
	}
	if index < 0 {
		if len(c.entries) == 4 {
			copy(c.entries, c.entries[1:])
			c.entries = c.entries[:3]
		}
		c.entries = append(c.entries, markdownViewEntry{key: key})
		index = len(c.entries) - 1
	}
	entry := &c.entries[index]
	changed := entry.source != source || entry.final != final
	if changed {
		if !strings.HasPrefix(source, entry.source) {
			entry.stream = markdownStream{}
		}
		cost := c.cost()
		c.parser.availableBytes = max(0, markdownMemoryLimit-cost.bytes)
		entry.stream.advance(&c.parser, source, final, max(0, markdownNodeLimit-cost.nodes))
		entry.source = source
		entry.final = final
	}
	if !changed && entry.layout.failure {
		return markdownLayoutLines(entry.layout, source, width)
	}
	if changed || entry.width != width || entry.color != c.color {
		entry.layout = markdownLayout{}
		available := (markdownMemoryLimit - c.cost().bytes) / 2
		if available <= 0 {
			entry.layout = markdownLayout{failure: true}
		} else {
			entry.layout = entry.stream.layoutBudget(width, c.color, available, max(0, markdownNodeLimit-c.cost().nodes+entry.stream.cost().nodes))
		}
		entry.width = width
		entry.color = c.color
	}
	if cost := c.cost(); cost.nodes > markdownNodeLimit || cost.bytes > markdownMemoryLimit {
		entry.stream = markdownStream{}
		entry.layout = markdownLayout{failure: true}
	}
	return markdownLayoutLines(entry.layout, source, width)
}
func markdownLayoutLines(layout markdownLayout, source string, width int) []string {
	if layout.failure {
		excerpt := source
		if len(excerpt) > 512 {
			excerpt = excerpt[:512]
			for !utf8.ValidString(excerpt) {
				excerpt = excerpt[:len(excerpt)-1]
			}
		}
		rows := markdownRawRows(excerpt, width, "Markdown unavailable · display budget · Ctrl+O/r for raw text")
		lines := make([]string, len(rows))
		for i, row := range rows {
			lines[i] = row.text
		}
		return lines
	}
	lines := make([]string, len(layout.rows))
	for i, row := range layout.rows {
		lines[i] = row.text
	}
	return lines
}
func (model *Model) prepareMarkdownView() {
	previousBytes := cap(model.markdown.lines)*16 + cap(model.reading.anchors)*int(unsafe.Sizeof(transcriptAnchor{}))
	for _, line := range model.markdown.lines {
		previousBytes += len(line)
	}
	model.markdown.lines = nil
	model.markdown.externalBytes = cap(model.main.handoff) * 16
	for _, line := range model.main.handoff {
		model.markdown.externalBytes += len(line)
	}
	model.markdown.externalBytes += cap(model.browser.markdown.rows) * 24
	for _, row := range model.browser.markdown.rows {
		model.markdown.externalBytes += len(row.text)
	}
	baseExternal := model.markdown.externalBytes
	model.markdown.externalBytes += previousBytes
	messages := model.visibleTranscript()
	if model.main.handoff != nil {
		messages = model.transcript
	}
	// 先固定本窗口的四份派生文档，避免逐项 LRU 使长历史每帧全部重解析。
	selected := make(map[markdownViewKey]bool)
	if model.detailOpen && model.browser.detail && model.browser.selected < len(model.browser.items) {
		selected[markdownViewKey{cursor: model.browser.items[model.browser.selected].Cursor, reasoning: model.browser.items[model.browser.selected].temporary}] = true
	}
	keyFor := func(index int, message transcriptMessage) markdownViewKey {
		key := markdownViewKey{cursor: message.cursor}
		if !key.cursor.Valid() {
			key.generation = model.main.generation
			key.local = message.local
			if key.local == 0 {
				key.local = uint64(index + 1)
			}
		}
		return key
	}
	if model.reading.detached {
		selected[model.reading.anchor.key] = true
	}
	visible := make(map[markdownViewKey]bool)
	start := max(0, len(model.reading.anchors)-model.transcriptHeight())
	if model.reading.detached {
		start = model.reading.scroll
	}
	for i := start; i < min(len(model.reading.anchors), start+model.transcriptHeight()); i++ {
		visible[model.reading.anchors[i].key] = true
	}
	for _, entry := range model.markdown.entries {
		if visible[entry.key] && len(selected) < 4 {
			selected[entry.key] = true
		}
	}
	for i := len(messages) - 1; i >= 0 && len(selected) < 4; i-- {
		if messages[i].role == "Assistant" {
			selected[keyFor(i, messages[i])] = true
		}
	}
	retained := model.markdown.entries[:0]
	for _, entry := range model.markdown.entries {
		if selected[entry.key] {
			retained = append(retained, entry)
		}
	}
	clear(model.markdown.entries[len(retained):])
	model.markdown.entries = retained
	// 每条助手消息独立准备；工具仍通过既有 presenter 处理。
	lines := append([]string(nil), model.main.handoff...)
	var anchors []transcriptAnchor
	for range lines {
		anchors = append(anchors, model.reading.anchor)
	}
	var group []transcriptMessage
	flush := func() {
		rendered := renderTranscript(group, model.widthOrDefault())
		lines = append(lines, rendered...)
		key := markdownViewKey{}
		if len(group) > 0 {
			key.cursor = group[0].cursor
		}
		for range rendered {
			anchors = append(anchors, transcriptAnchor{key: key})
		}
		group = nil
	}
	for index, message := range messages {
		if message.activity != nil || message.role != "Assistant" {
			group = append(group, message)
			continue
		}
		flush()
		key := keyFor(index, message)
		model.markdown.lines = lines
		model.markdown.externalBytes = baseExternal + previousBytes + cap(anchors)*int(unsafe.Sizeof(transcriptAnchor{}))
		var body []string
		var rawRows []markdownRow
		text, complete := model.assistantBody(message)
		if !complete || message.excerpt || !selected[key] {
			for i, entry := range model.markdown.entries {
				if entry.key == key {
					model.markdown.entries = append(model.markdown.entries[:i:i], model.markdown.entries[i+1:]...)
					break
				}
			}
			notice := "Text excerpt · Ctrl+O for full text"
			switch model.main.attempted[message.cursor] {
			case hydrationFailed:
				notice = "Body unavailable · Ctrl+O to retry"
			case hydrationPaged:
				notice = "Paged text excerpt · Ctrl+O for full text"
			case hydrationUnrequested:
				if message.more && len(model.browser.cache) == 4 {
					notice = "Text excerpt · display cache budget · Ctrl+O for full text"
				}
			}
			if !selected[key] {
				notice = "Markdown collapsed · display budget · Ctrl+O for full text"
			}
			if message.excerpt {
				notice = "Streaming excerpt · Ctrl+O after commit"
			}
			excerpt := message.text
			if len(excerpt) > 512 && !message.excerpt {
				excerpt = excerpt[:512]
				for !utf8.ValidString(excerpt) {
					excerpt = excerpt[:len(excerpt)-1]
				}
			}
			if message.excerpt {
				remaining := markdownMemoryLimit - model.markdown.cost().bytes
				height := max(model.transcriptHeight(), (remaining-6*len(excerpt))/192)
				anchor := -1
				if model.reading.detached && model.reading.anchor.key == key {
					anchor = max(0, model.reading.anchor.source-message.sourceStart)
				}
				var first int
				rawRows, first = markdownRawWindow(excerpt, max(1, model.widthOrDefault()-2), len(excerpt)*6+1, height, anchor)
				if first > 0 {
					notice = "Streaming excerpt · display budget · Ctrl+O after commit"
				}
				rawRows = append([]markdownRow{{text: notice}}, rawRows...)
			} else {
				rawRows = markdownRawRows(excerpt, max(1, model.widthOrDefault()-2), notice)
				if !selected[key] && len(rawRows) > 4 {
					rawRows = rawRows[:4]
				}
			}
			body = make([]string, len(rawRows))
			for i, row := range rawRows {
				body[i] = row.text
			}
		} else {
			body = model.markdown.prepare(key, text, max(1, model.widthOrDefault()-2), model.state != stateStreaming || message.cursor.Valid() || index != len(messages)-1)
		}
		// 派生行已就绪，组合前另行预留主线字符串、锚点及两个数组可能增长的容量。
		rowCount := len(body) + 1
		projection := rowCount * 4
		if len(lines)+rowCount > cap(lines) {
			projection += max(2*cap(lines), len(lines)+rowCount) * 16
		}
		if len(anchors)+rowCount > cap(anchors) {
			projection += max(2*cap(anchors), len(anchors)+rowCount) * int(unsafe.Sizeof(transcriptAnchor{}))
		}
		for _, line := range body {
			projection += len(line)
		}
		if projection > markdownMemoryLimit-model.markdown.cost().bytes && len(body) > 2 {
			body = []string{"Display budget · Ctrl+O for text", body[1]}
			rawRows = []markdownRow{{}, {}}
		}
		if message.unfinished {
			lines = append(lines, "● Assistant (unfinished)")
			anchors = append(anchors, transcriptAnchor{key: key, source: message.sourceStart})
		}
		for i, line := range body {
			prefix := "  "
			if i == 0 {
				prefix = "● "
			}
			lines = append(lines, prefix+line)
			offset := message.sourceStart
			if rawRows != nil && i < len(rawRows) {
				offset += rawRows[i].source
			}
			for _, entry := range model.markdown.entries {
				if entry.key == key && i < len(entry.layout.rows) {
					offset = message.sourceStart + entry.layout.rows[i].source
					break
				}
			}
			anchors = append(anchors, transcriptAnchor{key, offset})
		}
	}
	flush()
	model.markdown.lines = lines
	model.markdown.externalBytes = baseExternal + cap(anchors)*int(unsafe.Sizeof(transcriptAnchor{}))
	model.reading.restore(anchors)
	if len(messages) > 0 {
		last := messages[len(messages)-1]
		key := keyFor(len(messages)-1, last)
		if model.reading.detached && (key != model.reading.lastKey || len(last.text) != model.reading.lastLength) {
			model.reading.newContent = true
		}
		model.reading.lastKey = key
		model.reading.lastLength = len(last.text)
	}
	model.browser.anchorCursor = model.reading.anchor.key.cursor
	model.browser.visibleCursors = nil
	start = max(0, len(anchors)-model.transcriptHeight())
	if model.reading.detached {
		start = model.reading.scroll
	}
	for i := start; i < min(len(anchors), start+model.transcriptHeight()); i++ {
		cursor := anchors[i].key.cursor
		if cursor.Valid() {
			model.browser.visibleCursors = append(model.browser.visibleCursors, cursor)
		}
	}
}

// WithMarkdownColor 由宿主显式注入样式选择，不读取环境或探测终端。
func (model Model) WithMarkdownColor(enabled bool) Model {
	model.markdown.color = enabled
	model.prepareMarkdownView()
	return model
}

func markdownRawRows(source string, width int, notice string) []markdownRow {
	renderer := markdownRenderer{width: max(1, width), limit: markdownMemoryLimit}
	renderer.add(source, 0, "")
	renderer.flush("")
	rows := renderer.layout.rows
	if notice != "" {
		rows = append([]markdownRow{{text: notice}}, rows...)
	}
	if renderer.layout.failure {
		rows = append(rows, markdownRow{text: "Display excerpt · resize or open raw text"})
	}
	return rows
}
