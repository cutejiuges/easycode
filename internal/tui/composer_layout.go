package tui

import (
	"fmt"
	"strings"

	"github.com/rivo/uniseg"
)

// composerPoint 使用连续值数组记录字素边界，不为每个字符分配对象。
type composerPoint struct{ offset, column, display int32 }
type composerRow struct {
	first, last int
	display     string
}
type composerLayout struct {
	source string
	width  int
	points []composerPoint
	rows   []composerRow
}

func composerControl(r rune) bool {
	return r < 32 || r >= 127 && r <= 159 || r == 0x61c || r == 0x200e || r == 0x200f || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069
}

func displayComposerCluster(cluster string, column int) (string, int) {
	if cluster == "\t" {
		n := 4 - column%4
		return strings.Repeat(" ", n), n
	}
	for _, r := range cluster {
		if composerControl(r) {
			var text strings.Builder
			for _, value := range cluster {
				if composerControl(value) {
					fmt.Fprintf(&text, "\\u%04x", value)
				} else {
					text.WriteRune(value)
				}
			}
			shown := text.String()
			return shown, uniseg.StringWidth(shown)
		}
	}
	width := uniseg.StringWidth(cluster)
	if width == 0 {
		return "◌" + cluster, 1
	}
	return cluster, width
}

// nextComposerCluster 为常见 ASCII 提供无分配路径，同时保留组合字符边界。
func nextComposerCluster(text string, state int) (string, string, int) {
	if text[0] >= 32 && text[0] < 127 && (len(text) == 1 || text[1] < 128) {
		return text[:1], text[1:], -1
	}
	cluster, rest, _, next := uniseg.FirstGraphemeClusterInString(text, state)
	return cluster, rest, next
}

func buildComposerLayout(text string, width int) *composerLayout {
	layout := &composerLayout{source: text, width: width, points: make([]composerPoint, 0, len(text)+1)}
	var line strings.Builder
	first, column, offset, state := 0, 0, 0, -1
	finish := func() {
		layout.rows = append(layout.rows, composerRow{first, len(layout.points), line.String()})
		first = len(layout.points)
		column = 0
		line = strings.Builder{}
	}
	rest := text
	for rest != "" {
		var cluster string
		cluster, rest, state = nextComposerCluster(rest, state)
		if column >= width {
			finish()
		}
		if cluster == "\n" {
			layout.points = append(layout.points, composerPoint{int32(offset), int32(column), int32(line.Len())})
			offset += len(cluster)
			finish()
			continue
		}
		shown, cells := displayComposerCluster(cluster, column)
		if column > 0 && column+cells > width {
			finish()
			shown, cells = displayComposerCluster(cluster, 0)
		}
		if cells > width {
			shown = "?"
			cells = 1
		}
		layout.points = append(layout.points, composerPoint{int32(offset), int32(column), int32(line.Len())})
		line.WriteString(shown)
		column += cells
		offset += len(cluster)
	}
	if column >= width {
		finish()
	}
	layout.points = append(layout.points, composerPoint{int32(offset), int32(column), int32(line.Len())})
	finish()
	return layout
}

func safeComposerText(text string) string {
	var result strings.Builder
	column, state := 0, -1
	for text != "" {
		var cluster string
		cluster, text, state = nextComposerCluster(text, state)
		if cluster == "\n" {
			result.WriteByte('\n')
			column = 0
			continue
		}
		shown, width := displayComposerCluster(cluster, column)
		result.WriteString(shown)
		column += width
	}
	return result.String()
}

func (c composer) view(editable bool) string {
	if c.width == 1 {
		if editable {
			return "|\n"
		}
		return " \n"
	}
	var result strings.Builder
	rowIndex := c.rowIndex()
	point := c.layout.points[c.pointIndex()]
	end := min(len(c.layout.rows), c.top+c.visibleRows())
	prefix := ""
	if c.width >= 4 {
		prefix = "> "
	}
	for i := c.top; i < end; i++ {
		line := c.layout.rows[i].display
		result.WriteString(prefix)
		if editable && i == rowIndex {
			split := int(point.display)
			result.WriteString(line[:split])
			result.WriteByte('|')
			result.WriteString(line[split:])
		} else {
			result.WriteString(line)
		}
		result.WriteByte('\n')
	}
	if c.height >= 5 && c.width >= 24 {
		hint := "Enter: send | Alt+Enter/Ctrl+J: newline"
		if c.top > 0 || end < len(c.layout.rows) {
			hint = fmt.Sprintf("Lines %d-%d/%d | %s", c.top+1, end, len(c.layout.rows), hint)
		}
		result.WriteString(hint[:min(len(hint), c.width)])
		result.WriteByte('\n')
	}
	return result.String()
}
