package tui

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"easycode/internal/protocol"
)

// composer 的布局和历史切片发布后不可变，值复制即为独立的编辑快照。
type composer struct {
	text          string
	cursor        int
	preferred     int
	top           int
	width, height int
	layout        *composerLayout
	history       composerHistory
}

func newComposer() composer {
	c := composer{preferred: -1, width: 80, height: 24}
	c.relayout()
	return c
}

func normalizeComposerText(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

func (c *composer) replace(start, end int, inserted string) error {
	inserted = normalizeComposerText(inserted)
	if !utf8.ValidString(inserted) {
		return fmt.Errorf("input is not valid UTF-8")
	}
	if len(c.text)-(end-start)+len(inserted) > protocol.MaxInputBytes {
		return fmt.Errorf("input exceeds %d bytes", protocol.MaxInputBytes)
	}
	if start == end && inserted == "" {
		return nil
	}
	c.text = c.text[:start] + inserted + c.text[end:]
	c.cursor = start + len(inserted)
	c.preferred = -1
	c.history.endNavigation()
	c.relayout()
	// 插入的组合字符可与相邻字素合并，向右归位到新的合法边界。
	index := sort.Search(len(c.layout.points), func(i int) bool { return int(c.layout.points[i].offset) >= c.cursor })
	c.cursor = int(c.layout.points[index].offset)
	c.reveal()
	return nil
}

func (c *composer) resize(width, height int) {
	c.width, c.height = max(1, width), max(1, height)
	c.preferred = -1
	c.relayout()
}

func (c *composer) relayout() {
	width := max(1, c.width-3)
	if c.layout == nil || c.layout.source != c.text || c.layout.width != width {
		c.layout = buildComposerLayout(c.text, width)
	}
	c.reveal()
}

func (c composer) pointIndex() int {
	return sort.Search(len(c.layout.points), func(i int) bool { return int(c.layout.points[i].offset) >= c.cursor })
}

func (c composer) rowIndex() int {
	point := c.pointIndex()
	return sort.Search(len(c.layout.rows), func(i int) bool { return c.layout.rows[i].last > point })
}

func (c composer) visibleRows() int { return min(8, max(1, c.height-4)) }

func (c *composer) reveal() {
	row := c.rowIndex()
	c.top = min(c.top, row)
	c.top = max(c.top, row-c.visibleRows()+1)
	c.top = max(0, min(c.top, len(c.layout.rows)-c.visibleRows()))
}

func (c *composer) moveTo(offset int) {
	c.cursor = offset
	c.preferred = -1
	c.reveal()
}

func (c *composer) horizontal(delta int) {
	index := min(max(0, c.pointIndex()+delta), len(c.layout.points)-1)
	c.moveTo(int(c.layout.points[index].offset))
}

func (c *composer) vertical(delta int) {
	row := c.rowIndex()
	target := row + delta
	if target < 0 || target >= len(c.layout.rows) {
		c.recall(delta)
		return
	}
	if c.preferred < 0 {
		c.preferred = int(c.layout.points[c.pointIndex()].column)
	}
	line := c.layout.rows[target]
	index := line.first
	for index+1 < line.last && int(c.layout.points[index+1].column) <= c.preferred {
		index++
	}
	c.cursor = int(c.layout.points[index].offset)
	c.reveal()
}

func (c *composer) erase(backward bool) error {
	index := c.pointIndex()
	if backward {
		if index == 0 {
			return nil
		}
		return c.replace(int(c.layout.points[index-1].offset), c.cursor, "")
	}
	if index+1 == len(c.layout.points) {
		return nil
	}
	return c.replace(c.cursor, int(c.layout.points[index+1].offset), "")
}

func (c *composer) clear() {
	c.text = ""
	c.cursor = 0
	c.top = 0
	c.preferred = -1
	c.history.endNavigation()
	c.relayout()
}
