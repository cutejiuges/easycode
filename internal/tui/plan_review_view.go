package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

const (
	planReviewImplement = "Implement with normal approvals"
	planReviewMinWidth  = len("> " + planReviewImplement)
)

type planReviewViewport struct {
	source        string
	forceRaw      bool
	raw           bool
	preparedRaw   bool
	width, height int
	first, shown  int
	markdownRows  []markdownRow
	page          []markdownRow
}

func newPlanReviewViewport(markdown string) planReviewViewport {
	var source strings.Builder
	forceRaw := false
	for rest, state := markdown, -1; rest != ""; {
		var cluster string
		cluster, rest, state = nextComposerCluster(rest, state)
		_, cells := displayComposerCluster(cluster, 0)
		wide := cells > planReviewMinWidth
		for _, r := range cluster {
			switch {
			case r == '\n':
				source.WriteByte('\n')
			case r == '\\':
				source.WriteString(`\\`)
			case wide || unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
				fmt.Fprintf(&source, `\u{%04X}`, r)
				forceRaw = true
			default:
				source.WriteRune(r)
			}
		}
	}
	return planReviewViewport{source: source.String(), forceRaw: forceRaw}
}

func (model Model) planReviewBodyRows() int { return max(1, model.heightOrDefault()-5) }

func (model Model) planReviewFits() bool {
	return model.widthOrDefault() >= planReviewMinWidth && model.heightOrDefault() >= 6
}

// preparePlanReview 在 Update 中缓存完整布局及当前页，滚动不重新解析正文。
func (model *Model) preparePlanReview() {
	if !model.planReview.open || !model.planReviewFits() {
		return
	}
	viewport := &model.planReview.viewport
	width, height := model.widthOrDefault(), model.planReviewBodyRows()
	useRaw := viewport.raw || viewport.forceRaw
	if viewport.width != width || viewport.preparedRaw != useRaw {
		viewport.width, viewport.preparedRaw = width, useRaw
		viewport.markdownRows, viewport.page = nil, nil
		if !useRaw {
			key := markdownViewKey{planReview: true}
			model.markdown.prepare(key, model.planReview.offer.Markdown, width, true)
			for _, entry := range model.markdown.entries {
				if entry.key == key && !entry.layout.failure {
					viewport.markdownRows = entry.layout.rows
					break
				}
			}
		}
	}
	if viewport.page != nil && viewport.height == height && viewport.shown == viewport.first {
		return
	}
	viewport.height = height
	if len(viewport.markdownRows) == 0 {
		viewport.page, viewport.first = markdownRawWindow(viewport.source, width, viewport.first, height, -1)
	} else {
		viewport.first = min(viewport.first, max(0, len(viewport.markdownRows)-height))
		viewport.page = viewport.markdownRows[viewport.first:min(len(viewport.markdownRows), viewport.first+height)]
	}
	viewport.shown = viewport.first
}

func (model Model) renderPlanReview() string {
	width, height := model.widthOrDefault(), model.heightOrDefault()
	if !model.planReviewFits() {
		return hookPanel([]string{"Plan review · resize", "Esc: keep planning"}, width, height, 0)
	}
	view := model.planReview
	title := "Review plan · Markdown"
	if len(view.viewport.markdownRows) == 0 {
		title = "Review plan · source"
	}
	lines := []string{title}
	for _, row := range view.viewport.page {
		lines = append(lines, row.text)
	}
	keep, implement := "> ", "  "
	if view.implement {
		keep, implement = implement, keep
	}
	lines = append(lines, keep+"Keep planning", implement+planReviewImplement)
	if view.confirmation != nil {
		lines = append(lines, "Confirming · Esc cancels wait")
	} else if view.viewport.forceRaw {
		lines = append(lines, "↑↓ · Enter · PgUp/Dn · Esc")
	} else {
		lines = append(lines, "↑↓ · Enter · PgUp/Dn · r · Esc")
	}
	if model.errorSummary != "" {
		lines = append(lines, ansi.Truncate(model.errorSummary, width, "…"))
	}
	return strings.Join(lines, "\n")
}
