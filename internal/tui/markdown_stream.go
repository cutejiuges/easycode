package tui

import (
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"strings"
	"unsafe"
)

type markdownFragment struct {
	document markdownDocument
	offset   int
}
type markdownStream struct {
	references bool
	codeOpen   bool
	codeMark   string
	codeBody   int
	rawCode    string
	fragments  []markdownFragment
	stable     int
	parsed     int
	tail       markdownDocument
	preview    string
	final      bool
}

// advance 只在完整行或收口时解析活动尾块；跨块引用出现时重新建立完整语义基准。
func (s *markdownStream) advance(p *markdownParser, source string, final bool, budget int) {
	end := len(source)
	if !final {
		start := min(s.parsed, len(source))
		end = start + strings.LastIndexByte(source[start:], '\n') + 1
	}
	preview := source[end:]
	if len(preview) > 8<<10 {
		preview = preview[len(preview)-(8<<10):]
		for len(preview) > 0 && preview[0]&0xc0 == 0x80 {
			preview = preview[1:]
		}
		preview = "[Unfinished line excerpt] " + preview
	}
	if !final {
		if target := strings.LastIndex(preview, "]("); target >= 0 && !strings.Contains(preview[target+2:], ")") {
			preview = preview[:target+1]
		}
		defer func() {
			if s.tail.root != nil {
				if _, table := s.tail.root.LastChild().(*east.Table); table {
					s.preview = ""
				}
			}
		}()
	}
	s.preview = preview
	if end == s.parsed && !(final && s.codeOpen) {
		if final {
			s.tail.plainCode = nil
		}
		s.final = final
		return
	}
	if s.codeOpen && !final {
		closed := false
		for line := range strings.SplitSeq(source[s.parsed:end], "\n") {
			line = strings.TrimRight(line, " \t\r")
			indent := 0
			for indent < len(line) && line[indent] == ' ' {
				indent++
			}
			if indent > 3 {
				continue
			}
			line = line[indent:]
			if strings.HasPrefix(line, s.codeMark) && strings.Trim(line, string(s.codeMark[0])) == "" {
				closed = true
				break
			}
		}
		if !closed {
			s.rawCode = source[s.codeBody:end]
			s.parsed = end
			return
		}
	}
	previous := *s
	defer s.reuseCodes(&previous)
	if s.codeOpen {
		s.codeOpen = false
		s.rawCode = ""
	}
	if end < s.parsed {
		s.fragments = nil
		s.stable = 0
	}
	s.final = final
	s.references = s.references || strings.Contains(source[min(end, max(0, s.parsed-1)):end], "]:")
	s.parsed = end
	if s.references {
		s.fragments = nil
		s.stable = 0
		s.tail = p.parse(source[:end], budget)
		s.tail.markOpenFence(final)
		return
	}
	used := 0
	for _, fragment := range s.fragments {
		used += fragment.document.nodes
	}
	tail := p.parse(source[s.stable:end], budget-used)
	if !final && !tail.failure && tail.root.ChildCount() > 1 {
		cut := markdownLastBlockStart(tail)
		if cut > 0 {
			originalLimit := p.availableBytes
			defer func() { p.availableBytes = originalLimit }()
			p.availableBytes -= markdownDocumentCost(tail).bytes
			stable := p.parse(source[s.stable:s.stable+cut], budget-used-tail.nodes)
			if !stable.failure {
				p.availableBytes -= markdownDocumentCost(stable).bytes
				if len(s.fragments) == cap(s.fragments) {
					capacity := max(1, len(s.fragments)*2)
					allocation := capacity * int(unsafe.Sizeof(markdownFragment{}))
					if allocation > p.availableBytes {
						s.tail = tail
						s.tail.markOpenFence(final)
						return
					}
					next := make([]markdownFragment, len(s.fragments), capacity)
					copy(next, s.fragments)
					s.fragments = next
					p.availableBytes -= allocation
				}
				s.fragments = append(s.fragments, markdownFragment{stable, s.stable})
				s.stable += cut
				used += stable.nodes
				tail = p.parse(source[s.stable:end], budget-used-tail.nodes)
			}
		}
	}
	tail.markOpenFence(final)
	s.tail = tail
	if !final && !tail.failure && tail.plainCode != nil && tail.root.ChildCount() == 1 {
		lineEnd := strings.IndexByte(source[s.stable:end], '\n')
		if lineEnd >= 0 {
			line := strings.TrimRight(source[s.stable:s.stable+lineEnd], " \t\r")
			mark := ""
			if strings.HasPrefix(line, "```") {
				mark = "```"
			} else if strings.HasPrefix(line, "~~~") {
				mark = "~~~"
			}
			if mark != "" {
				length := len(mark)
				for length < len(line) && line[length] == mark[0] {
					length++
				}
				s.codeMark = line[:length]
				s.codeBody = s.stable + lineEnd + 1
				s.codeOpen = true
				s.rawCode = source[s.codeBody:end]
			}
		}
	}
}
func (tail *markdownDocument) markOpenFence(final bool) {
	if !final && !tail.failure && tail.root != nil {
		node := tail.root.LastChild()
		for node != nil && node.LastChild() != nil {
			node = node.LastChild()
		}
		if code, ok := node.(*ast.FencedCodeBlock); ok {
			closed := false
			for _, block := range tail.blocks {
				if block.node == code && block.closed {
					closed = true
				}
			}
			if !closed {
				tail.plainCode = code
			}
		}
	}
}
func markdownLastBlockStart(doc markdownDocument) int {
	node := doc.root.LastChild()
	if node == nil {
		return 0
	}
	first := -1
	_ = ast.Walk(node, func(n ast.Node, enter bool) (ast.WalkStatus, error) {
		if enter && n.Type() == ast.TypeBlock && n.Lines().Len() > 0 {
			v := n.Lines().At(0).Start
			if first < 0 || v < first {
				first = v
			}
		}
		return ast.WalkContinue, nil
	})
	for _, block := range doc.blocks {
		if block.node == node && (first < 0 || block.offset < first) {
			first = block.offset
		}
	}
	if first < 0 {
		return 0
	}
	start := strings.LastIndexByte(string(doc.source[:first]), '\n') + 1
	return doc.originalOffset(start)
}
func (s *markdownStream) layout(width int, color bool) markdownLayout {
	return s.layoutBudget(width, color, markdownMemoryLimit, markdownNodeLimit)
}

// merge 先预留行数组扩容时同时存活的容量，正文字符串直接移交。
func (result *markdownLayout) merge(part markdownLayout, offset, limit int) {
	if result.failure || part.failure || result.bytes+part.bytes > limit {
		*result = markdownLayout{failure: true}
		return
	}
	for i := range part.rows {
		part.rows[i].source += offset
	}
	if len(result.rows) == 0 {
		*result = part
		return
	}
	n := len(result.rows) + len(part.rows)
	rowSize := int(unsafe.Sizeof(markdownRow{}))
	if n > cap(result.rows) {
		capacity := max(n, 2*cap(result.rows))
		if result.bytes+part.bytes+capacity*rowSize > limit {
			capacity = n
		}
		if result.bytes+part.bytes+capacity*rowSize > limit {
			*result = markdownLayout{failure: true}
			return
		}
		rows := make([]markdownRow, len(result.rows), capacity)
		copy(rows, result.rows)
		result.bytes += (capacity - cap(result.rows)) * rowSize
		result.rows = rows
	}
	result.rows = append(result.rows, part.rows...)
	result.bytes += part.bytes - cap(part.rows)*rowSize
}
func (s *markdownStream) layoutBudget(width int, color bool, limit, nodeLimit int) markdownLayout {
	result := markdownLayout{}
	usedNodes := 0
	appendDocument := func(doc markdownDocument, offset int) {
		before := markdownDocumentCost(doc)
		part := renderMarkdownBudget(doc, width, color, max(0, limit-result.bytes), max(0, nodeLimit-usedNodes))
		after := markdownDocumentCost(doc)
		limit -= max(0, after.bytes-before.bytes)
		result.merge(part, offset, limit)
		usedNodes += after.nodes
	}
	for _, fragment := range s.fragments {
		appendDocument(fragment.document, fragment.offset)
		if result.failure {
			return result
		}
	}
	if s.codeOpen {
		remaining := limit - result.bytes
		safe, escapes, ok := markdownSafeSourceBudget(s.rawCode, remaining)
		if !ok {
			return markdownLayout{failure: true}
		}
		if len(escapes) > 0 {
			remaining -= len(safe) + cap(escapes)*int(unsafe.Sizeof(markdownSourceEscape{}))
		}
		r := markdownRenderer{width: width, limit: remaining}
		r.add(safe, 0, "")
		r.flush("  ")
		sourceMap := markdownDocument{escapes: escapes}
		for i := range r.layout.rows {
			r.layout.rows[i].source = sourceMap.originalOffset(r.layout.rows[i].source)
		}
		result.merge(r.layout, s.codeBody, result.bytes+remaining)
	} else if s.tail.root != nil || s.tail.failure {
		appendDocument(s.tail, s.stable)
	}
	if !result.failure && s.preview != "" {
		r := markdownRenderer{width: width, limit: max(0, limit-result.bytes)}
		r.add(s.preview, 0, "")
		r.flush("")
		result.merge(r.layout, s.parsed, limit)
	}
	return result
}
