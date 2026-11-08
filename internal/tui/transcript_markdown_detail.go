package tui

import "easycode/internal/domain"

type transcriptMarkdownDetail struct {
	first, height  int
	raw            bool
	preserveAnchor bool
	rows           []markdownRow
	notice         string
	sourceAnchor   int
	anchorKnown    bool
	source         string
	width          int
	renderedRaw    bool
}

func (model *Model) prepareMarkdownDetail() {
	b := &model.browser
	if !model.detailOpen || !b.detail || b.page == nil || b.selected >= len(b.items) || (b.items[b.selected].Kind != domain.TranscriptAssistant && b.items[b.selected].Kind != domain.TranscriptReasoning) || b.stream != "" {
		b.markdown.rows = nil
		return
	}
	page := b.page.body
	complete := page.Offset == 0 && page.NextOffset == page.TotalBytes
	if !b.markdown.anchorKnown {
		b.markdown.sourceAnchor = 0
		index := model.detailScroll - b.markdown.first
		if index >= 0 && index < len(b.markdown.rows) {
			b.markdown.sourceAnchor = b.markdown.rows[index].source
		}
		b.markdown.anchorKnown = true
	}
	if !b.markdown.preserveAnchor {
		b.markdown.notice = ""
	}
	sourceAnchor := b.markdown.sourceAnchor
	width := max(1, model.widthOrDefault()-4)
	raw := !complete || b.markdown.raw
	changed := b.markdown.source != page.Text || b.markdown.width != width || b.markdown.renderedRaw != raw || b.markdown.rows == nil
	height := max(1, model.heightOrDefault()-3)
	if raw && (model.detailScroll != b.markdown.first || b.markdown.height != height || b.markdown.preserveAnchor) {
		changed = true
	}
	if changed {
		b.markdown.source = page.Text
		b.markdown.width = width
		b.markdown.renderedRaw = raw
		if !complete || b.markdown.raw {
			anchor := -1
			if b.markdown.preserveAnchor {
				anchor = sourceAnchor
			}
			b.markdown.rows, b.markdown.first = markdownRawWindow(page.Text, width, model.detailScroll, height, anchor)
			model.detailScroll = b.markdown.first
			b.markdown.height = height
			b.markdown.preserveAnchor = false
			if anchor < 0 && len(b.markdown.rows) > 0 {
				b.markdown.sourceAnchor = b.markdown.rows[0].source
			}
		} else {
			b.markdown.first = 0
			key := markdownViewKey{cursor: b.items[b.selected].Cursor, reasoning: b.items[b.selected].temporary}
			lines := model.markdown.prepare(key, page.Text, max(1, model.widthOrDefault()-4), true)
			b.markdown.rows = make([]markdownRow, len(lines))
			for i, line := range lines {
				b.markdown.rows[i] = markdownRow{text: line}
			}
			for _, entry := range model.markdown.entries {
				if entry.key == key && len(entry.layout.rows) == len(lines) {
					copy(b.markdown.rows, entry.layout.rows)
					break
				}
			}
		}
	}
	if b.markdown.preserveAnchor {
		model.detailScroll = 0
		for i, row := range b.markdown.rows {
			if row.source <= sourceAnchor {
				model.detailScroll = i
			}
		}
		if len(b.markdown.rows) > 0 && b.markdown.rows[model.detailScroll].source != sourceAnchor {
			b.markdown.notice = "Nearest visible source row"
		}
		b.markdown.preserveAnchor = false
	}
}
