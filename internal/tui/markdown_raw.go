package tui

import (
	"fmt"
	"strings"
)

// markdownRawWindow 按原文偏移定位，仅物化可见行。极窄终端下无需为整页每个字素建立行对象。
func markdownRawWindow(source string, width, first, height, anchor int) ([]markdownRow, int) {
	width = max(1, width)
	height = max(1, height)
	count, position := 0, 0
	walkMarkdownRaw(source, width, func(row, offset int) bool {
		count = row + 1
		if offset <= anchor {
			position = row
		}
		return false
	}, nil)
	if anchor >= 0 {
		first = position
	}
	first = min(max(0, first), max(0, count-height))
	rows := make([]markdownRow, 0, min(height, count))
	walkMarkdownRaw(source, width, func(row, offset int) bool { return row >= first && row < first+height }, func(offset int, text string) { rows = append(rows, markdownRow{text, offset}) })
	return rows, first
}
func walkMarkdownRaw(source string, width int, selectRow func(int, int) bool, emit func(int, string)) {
	row, origin, column := 0, 0, 0
	write := selectRow(row, origin)
	var text strings.Builder
	finish := func(next int) {
		if write && emit != nil {
			emit(origin, text.String())
		}
		text = strings.Builder{}
		row++
		column = 0
		origin = next
		write = selectRow(row, origin)
	}
	put := func(shown string, cells, offset int) {
		if column > 0 && column+cells > width {
			finish(offset)
		}
		if cells > width {
			shown = "?"
			cells = 1
		}
		if write {
			text.WriteString(shown)
		}
		column += cells
	}
	rest, state := source, -1
	for rest != "" {
		offset := len(source) - len(rest)
		var cluster string
		cluster, rest, state = nextComposerCluster(rest, state)
		if cluster == "\n" || cluster == "\r\n" {
			finish(offset + len(cluster))
			continue
		}
		if cluster == "\t" {
			n := 4 - column%4
			for range n {
				put(" ", 1, offset)
			}
			continue
		}
		control := false
		for _, c := range cluster {
			if composerControl(c) {
				control = true
				break
			}
		}
		if control {
			for delta, c := range cluster {
				if composerControl(c) {
					escaped := fmt.Sprintf("\\u%04x", c)
					for i := range len(escaped) {
						put(escaped[i:i+1], 1, offset+delta)
					}
				} else {
					shown, cells := displayComposerCluster(string(c), column)
					put(shown, cells, offset+delta)
				}
			}
			continue
		}
		shown, cells := displayComposerCluster(cluster, column)
		put(shown, cells, offset)
	}
	if write && emit != nil {
		emit(origin, text.String())
	}
}
