package tui

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"unsafe"

	"github.com/rivo/uniseg"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
)

type markdownRun struct {
	text   string
	source int
	style  string
}
type markdownRow struct {
	text   string
	source int
}
type markdownLayout struct {
	rows    []markdownRow
	bytes   int
	failure bool
}
type markdownCodeLine struct{ display, source, padding int32 }

type markdownRenderer struct {
	prefixBytes  int
	codeLines    []markdownCodeLine
	activeTokens []highlightToken
	nodeLimit    int
	limit        int
	runBytes     int
	codeBytes    int
	plainCode    ast.Node
	codes        *markdownCodeCache
	source       []byte
	color        bool
	width        int
	layout       markdownLayout
	runs         []markdownRun
	nodes        int
}

func renderMarkdown(doc markdownDocument, width int, color bool) markdownLayout {
	return renderMarkdownBudget(doc, width, color, markdownMemoryLimit, markdownNodeLimit)
}
func renderMarkdownBudget(doc markdownDocument, width int, color bool, limit int, nodeLimit int) markdownLayout {
	if doc.failure || doc.root == nil || doc.nodes > nodeLimit {
		return markdownLayout{failure: true}
	}
	r := markdownRenderer{nodeLimit: nodeLimit, limit: limit, plainCode: doc.plainCode, codes: doc.codes, source: doc.source, color: color, width: max(1, width), nodes: doc.nodes}
	for node := doc.root.FirstChild(); node != nil; node = node.NextSibling() {
		r.block(node, "", 0)
	}
	for i := range r.layout.rows {
		r.layout.rows[i].source = doc.originalOffset(r.layout.rows[i].source)
	}
	return r.layout
}
func markdownStyle(kind highlightKind) string {
	switch kind {
	case highlightKeyword:
		return "35"
	case highlightString:
		return "32"
	case highlightComment:
		return "90"
	case highlightNumber:
		return "36"
	case highlightProperty, highlightVariable:
		return "33"
	default:
		return ""
	}
}
func (r *markdownRenderer) add(text string, source int, style string) {
	if text == "" || r.layout.failure {
		return
	}
	n := len(r.runs)
	size := int(unsafe.Sizeof(markdownRun{}))
	capacity := cap(r.runs)
	if n == capacity {
		capacity = max(8, capacity*2)
	}
	if r.prefixBytes+r.layout.bytes+r.runBytes+len(text)+(cap(r.runs)+capacity)*size+r.codeBytes > r.limit {
		r.layout.failure = true
		return
	}
	if n == cap(r.runs) {
		next := make([]markdownRun, n, capacity)
		copy(next, r.runs)
		r.runs = next
	}
	r.runs = append(r.runs, markdownRun{text, source, style})
	r.runBytes += len(text)
}
func (r *markdownRenderer) flush(prefix string) {
	if len(r.runs) == 0 || r.layout.failure {
		return
	}
	var line []byte
	scratch := r.prefixBytes + 2*r.runBytes + cap(r.runs)*int(unsafe.Sizeof(markdownRun{})) + r.codeBytes
	write := func(value string) {
		if r.layout.failure {
			return
		}
		needed := len(line) + len(value)
		if needed > cap(line) {
			capacity := max(needed, max(16, 2*cap(line)))
			if scratch+r.layout.bytes+cap(line)+capacity > r.limit {
				r.layout.failure = true
				return
			}
			next := make([]byte, len(line), capacity)
			copy(next, line)
			line = next
		}
		line = append(line, value...)
	}
	activeStyle := ""
	column, origin, used, state := 0, r.runs[0].source, false, -1
	finish := func() {
		if activeStyle != "" {
			write("\x1b[0m")
			activeStyle = ""
		}
		if r.layout.failure {
			return
		}
		size := int(unsafe.Sizeof(markdownRow{}))
		n := len(r.layout.rows)
		capacity := cap(r.layout.rows)
		if n == capacity {
			capacity = max(8, capacity*2)
		}
		cost := len(line)
		peak := scratch + r.layout.bytes + cap(line) + cost
		if n == cap(r.layout.rows) {
			peak += capacity * size
		}
		if peak > r.limit {
			r.layout.failure = true
			return
		}
		if n == cap(r.layout.rows) {
			next := make([]markdownRow, n, capacity)
			copy(next, r.layout.rows)
			r.layout.rows = next
			r.layout.bytes += (capacity - n) * size
		}
		r.layout.rows = append(r.layout.rows, markdownRow{string(line), origin})
		r.layout.bytes += cost
		line = line[:0]
		column = 0
		used = false
	}
	putPrefix := func() {
		if prefix != "" {
			for _, c := range prefix {
				if column >= r.width {
					break
				}
				write(string(c))
				column++
			}
		}
	}
	putPrefix()
	if r.prefixBytes+r.layout.bytes+2*r.runBytes+cap(r.runs)*int(unsafe.Sizeof(markdownRun{}))+r.codeBytes > r.limit {
		r.layout.failure = true
		return
	}
	var content strings.Builder
	content.Grow(r.runBytes)
	for _, run := range r.runs {
		content.WriteString(run.text)
	}
	remaining := content.String()
	position, runIndex, runStart, tokenIndex, codeLineIndex := 0, 0, 0, 0, 0
	for remaining != "" && !r.layout.failure {
		for runIndex+1 < len(r.runs) && position >= runStart+len(r.runs[runIndex].text) {
			runStart += len(r.runs[runIndex].text)
			runIndex++
		}
		run := r.runs[runIndex]
		var cluster string
		cluster, remaining, state = nextComposerCluster(remaining, state)
		originForCluster := run.source + position - runStart
		if len(r.codeLines) > 0 {
			for codeLineIndex+1 < len(r.codeLines) && int(r.codeLines[codeLineIndex+1].display) <= position {
				codeLineIndex++
			}
			codeLine := r.codeLines[codeLineIndex]
			originForCluster = int(codeLine.source) + max(0, position-int(codeLine.display)-int(codeLine.padding))
		}
		style := run.style
		boundary := runStart + len(run.text)
		for i := runIndex + 1; style == "" && i < len(r.runs) && boundary < position+len(cluster); i++ {
			style = r.runs[i].style
			boundary += len(r.runs[i].text)
		}
		if len(r.activeTokens) > 0 {
			for tokenIndex+1 < len(r.activeTokens) && int(r.activeTokens[tokenIndex].end) <= position {
				tokenIndex++
			}
			style = ""
			for i := tokenIndex; i < len(r.activeTokens) && int(r.activeTokens[i].start) < position+len(cluster) && style == ""; i++ {
				style = markdownStyle(r.activeTokens[i].kind)
			}
		}
		position += len(cluster)
		if !used {
			origin = originForCluster
		}
		if cluster == "\n" || cluster == "\r\n" {
			finish()
			putPrefix()
			continue
		}
		shown, cells := displayComposerCluster(cluster, column)
		if column+cells > r.width && used {
			finish()
			putPrefix()
			origin = originForCluster
			shown, cells = displayComposerCluster(cluster, column)
		}
		if column+cells > r.width {
			shown = "?"
			cells = 1
			if column >= r.width {
				finish()
				origin = originForCluster
			}
		}
		if r.color && activeStyle != style {
			if activeStyle != "" {
				write("\x1b[0m")
			}
			if style != "" {
				write("\x1b[" + style + "m")
			}
			activeStyle = style
		}
		write(shown)
		column += cells
		used = true

	}

	if used || len(r.layout.rows) == 0 {
		finish()
	}
	r.runs = nil
	r.runBytes = 0
}
func (r *markdownRenderer) inline(node ast.Node, style string) {
	switch n := node.(type) {
	case *ast.Text:
		raw := string(n.Segment.Value(r.source))
		if n.IsRaw() {
			r.add(raw, n.Segment.Start, style)
		} else {
			r.inlineText(raw, n.Segment.Start, style)
		}
		if n.HardLineBreak() {
			r.add("\n", n.Segment.Stop, style)
		} else if n.SoftLineBreak() {
			r.add(" ", n.Segment.Stop, style)
		}
		return
	case *ast.String:
		r.add(string(n.Value), 0, style)
		return
	case *ast.CodeSpan:
		style = "36"
	case *ast.Emphasis:
		if n.Level == 2 {
			style = "1"
		} else {
			style = "3"
		}
	case *east.Strikethrough:
		style = "9"
	case *east.TaskCheckBox:
		if n.IsChecked {
			r.add("[x] ", 0, "")
		} else {
			r.add("[ ] ", 0, "")
		}
		return
	case *ast.AutoLink:
		r.add(string(n.Label(r.source)), 0, "36")
		r.add(" ("+string(n.URL(r.source))+")", 0, "")
		return
	case *ast.RawHTML:
		for i := 0; i < n.Segments.Len(); i++ {
			segment := n.Segments.At(i)
			r.add(string(segment.Value(r.source)), segment.Start, style)
		}
		return
	}
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		r.inline(child, style)
	}
	switch n := node.(type) {
	case *ast.Link:
		r.add(" ("+html.UnescapeString(string(n.Destination))+")", 0, "")
	case *ast.Image:
		r.add(" ("+html.UnescapeString(string(n.Destination))+")", 0, "")
	}
}
func (r *markdownRenderer) inlineText(value string, source int, style string) {
	start := 0
	for i := 0; i < len(value); {
		end := i
		decoded := ""
		if value[i] == '\\' && i+1 < len(value) && strings.ContainsRune("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", rune(value[i+1])) {
			end = i + 2
			decoded = value[i+1 : end]
		}
		if value[i] == '&' {
			end = i + 1
			for end < len(value) {
				c := value[end]
				if c == ';' {
					end++
					break
				}
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '#') {
					break
				}
				end++
			}
			if end > i+1 {
				candidate := value[i:end]
				expanded := html.UnescapeString(candidate)
				if expanded != candidate {
					decoded = expanded
				}
			}
		}
		if decoded != "" {
			r.add(value[start:i], source+start, style)
			r.add(decoded, source+i, style)
			i = end
			start = i
		} else {
			if end > i {
				i = end
			} else {
				i++
			}
		}
	}
	r.add(value[start:], source+start, style)
}
func (r *markdownRenderer) block(node ast.Node, prefix string, depth int) {
	if r.layout.failure {
		return
	}
	if depth > markdownNodeLimit {
		r.layout.failure = true
		return
	}
	source := 0
	if node.Type() == ast.TypeBlock && node.Lines().Len() > 0 {
		source = node.Lines().At(0).Start
	}
	switch n := node.(type) {
	case *ast.Paragraph, *ast.TextBlock, *ast.Heading:
		style := ""
		if _, ok := node.(*ast.Heading); ok {
			style = "1"
		}
		r.inline(node, style)
		r.flush(prefix)
	case *ast.FencedCodeBlock:
		r.code(n, markdownBlockText(n, r.source), string(n.Language(r.source)), prefix, source)
	case *ast.CodeBlock:
		r.code(n, markdownBlockText(n, r.source), "", prefix, source)
	case *ast.HTMLBlock:
		r.add(markdownBlockText(n, r.source), source, "")
		if n.HasClosure() {
			r.add(string(n.ClosureLine.Value(r.source)), n.ClosureLine.Start, "")
		}
		r.flush(prefix)
	case *ast.Blockquote:
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			r.prefixedBlock(child, prefix, "│ ", depth+1)
		}
	case *ast.List:
		index := n.Start
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			marker := "• "
			if n.IsOrdered() {
				marker = strconv.Itoa(index) + ". "
				index++
			}
			r.prefixedBlock(child, prefix, marker, depth+1)
		}
	case *ast.ListItem:
		first := true
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			p := prefix
			if !first {
				p = strings.Repeat(" ", uniseg.StringWidth(prefix))
			}
			r.block(child, p, depth+1)
			first = false
		}
	case *ast.ThematicBreak:
		r.add(strings.Repeat("─", max(1, min(12, r.width-len(prefix)))), source, "")
		r.flush(prefix)
	case *east.Table:
		r.table(n, prefix, source)
	default:
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			r.block(child, prefix, depth+1)
		}
	}
}
func (r *markdownRenderer) code(node ast.Node, value, language, prefix string, source int) {
	count := node.Lines().Len()
	mappingBytes := count * int(unsafe.Sizeof(markdownCodeLine{}))
	if mappingBytes+r.layout.bytes+r.codeBytes > r.limit {
		r.layout.failure = true
		return
	}
	r.codeLines = make([]markdownCodeLine, count)
	r.codeBytes += mappingBytes
	offset := 0
	for i := 0; i < count; i++ {
		segment := node.Lines().At(i)
		r.codeLines[i] = markdownCodeLine{int32(offset), int32(segment.Start), int32(segment.Padding)}
		offset += len(segment.Value(r.source))
	}
	defer func() { r.codeLines = nil; r.codeBytes -= mappingBytes }()
	if r.color && node != r.plainCode && codeLanguage(language) != highlightPlain {
		result := highlightResult{failure: highlightScanFailed}
		found := false
		for _, cached := range r.codes.items {
			if cached.node == node {
				result = cached.result
				found = true
				break
			}
		}
		if !found {
			result = scanHighlight(value, codeLanguage(language), highlightLimits{128 * (len(value) + 1), max(0, r.nodeLimit-r.nodes), max(0, r.limit-r.prefixBytes-r.layout.bytes-r.codeBytes-r.runBytes), 64})
			r.codeBytes += result.capacity + int(unsafe.Sizeof(markdownCode{}))
			r.codes.scans++
			r.codes.items = append(r.codes.items, markdownCode{node, result})
		}
		if result.failure == highlightOK {
			r.add(value, source, "")
			r.activeTokens = result.tokens
			r.nodes += len(result.tokens)
			r.flush(prefix + "  ")
			r.activeTokens = nil
			return
		}
		reason := "Highlighting unavailable: scan failed"
		if result.failure == highlightBudgetExceeded {
			reason = "Highlighting unavailable: budget exceeded"
		}
		r.add(reason, source, "")
		r.flush(prefix)
	}
	r.add(value, source, "")
	r.flush(prefix + "  ")
}

type markdownTableCell struct {
	text   string
	source int
}

func (r *markdownRenderer) table(table *east.Table, prefix string, source int) {
	// 表格的行列元数据先精确计数，再一次分配；临时字符串也占用当前布局预算。
	count, rowCount := 0, 0
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		rowCount++
		count += row.ChildCount()
	}
	scratch := count*int(unsafe.Sizeof(markdownTableCell{})) + rowCount*24
	if r.prefixBytes+r.layout.bytes+r.codeBytes+scratch > r.limit {
		r.layout.failure = true
		return
	}
	r.codeBytes += scratch
	defer func() { r.codeBytes -= scratch }()
	cells := make([]markdownTableCell, 0, count)
	rows := make([][]markdownTableCell, 0, rowCount)
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		start := len(cells)
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			r.inline(cell, "")
			if r.layout.failure {
				return
			}
			if r.prefixBytes+r.layout.bytes+r.codeBytes+2*r.runBytes+cap(r.runs)*int(unsafe.Sizeof(markdownRun{})) > r.limit {
				r.layout.failure = true
				return
			}
			var text strings.Builder
			text.Grow(r.runBytes)
			origin := source
			if len(r.runs) > 0 {
				origin = r.runs[0].source
			}
			for _, run := range r.runs {
				text.WriteString(run.text)
			}
			value := text.String()
			scratch += text.Cap()
			r.codeBytes += text.Cap()
			r.runs = nil
			r.runBytes = 0
			cells = append(cells, markdownTableCell{value, origin})
		}
		rows = append(rows, cells[start:])
	}
	if len(rows) == 0 || len(rows[0]) == 0 {
		return
	}
	columns := len(rows[0])
	available := r.width - uniseg.StringWidth(prefix)
	cellWidth := (available-columns-1)/columns - 2
	vertical := cellWidth < 3
	if !vertical {
		for _, row := range rows {
			for _, cell := range row {
				if !markdownFitsRows(cell.text, cellWidth, 4) {
					vertical = true
				}
			}
		}
	}
	if vertical {
		data := rows[1:]
		if len(data) == 0 {
			data = rows[:1]
		}
		for i, row := range data {
			r.add(fmt.Sprintf("[%d]", i+1), row[0].source, "1")
			r.flush(prefix)
			for j, cell := range row {
				label := rows[0][j].text
				duplicate := label == ""
				for k := range rows[0] {
					if k != j && rows[0][k].text == label {
						duplicate = true
					}
				}
				if duplicate {
					label = fmt.Sprintf("Column %d", j+1)
				}
				r.add(label, cell.source, "")
				r.add(": ", cell.source, "")
				r.add(cell.text, cell.source, "")
				r.flush(prefix)
			}
		}
		return
	}
	for _, row := range rows {
		// 每个单元格最多四行，直接使用有界字素布局，不构造 composer 的光标索引。
		temporary := len(row) * int(unsafe.Sizeof(markdownLayout{}))
		if r.codeBytes+r.layout.bytes+r.prefixBytes+temporary+max(0, available)*2 > r.limit || available > r.limit {
			r.layout.failure = true
			return
		}
		wrapped := make([]markdownLayout, len(row))
		r.codeBytes += temporary
		height := 1
		for i, cell := range row {
			cellRenderer := markdownRenderer{width: cellWidth, limit: max(0, r.limit-r.prefixBytes-r.codeBytes-r.layout.bytes)}
			cellRenderer.add(cell.text, cell.source, "")
			cellRenderer.flush("")
			if cellRenderer.layout.failure {
				r.layout.failure = true
				r.codeBytes -= temporary
				return
			}
			wrapped[i] = cellRenderer.layout
			temporary += cellRenderer.layout.bytes
			r.codeBytes += cellRenderer.layout.bytes
			height = max(height, len(wrapped[i].rows))
		}
		for line := 0; line < height; line++ {
			r.add("│", row[0].source, "")
			for i, cell := range wrapped {
				value := ""
				origin := row[i].source
				if line < len(cell.rows) {
					value = cell.rows[line].text
					origin = cell.rows[line].source
				}
				r.add(" ", origin, "")
				r.add(value, origin, "")
				r.add(strings.Repeat(" ", max(0, cellWidth-uniseg.StringWidth(value)))+" │", origin, "")
			}
			r.flush(prefix)
		}
		r.codeBytes -= temporary
	}
}

func markdownFitsRows(value string, width, limit int) bool {
	rows, column, state := 1, 0, -1
	for value != "" {
		var cluster string
		cluster, value, state = nextComposerCluster(value, state)
		if cluster == "\n" {
			rows++
			column = 0
		} else {
			_, cells := displayComposerCluster(cluster, column)
			if column > 0 && column+cells > width {
				rows++
				column = 0
			}
			column += cells
		}
		if rows > limit {
			return false
		}
	}
	return true
}

func (r *markdownRenderer) prefixedBlock(node ast.Node, prefix, suffix string, depth int) {
	size := len(prefix) + len(suffix)
	if r.prefixBytes+size+r.layout.bytes+r.codeBytes > r.limit {
		r.layout.failure = true
		return
	}
	r.prefixBytes += size
	r.block(node, prefix+suffix, depth)
	r.prefixBytes -= size
}
