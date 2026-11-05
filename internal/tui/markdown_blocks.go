package tui

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"unsafe"
)

type markdownBlockStart struct {
	closed bool
	node   ast.Node
	offset int
}
type markdownBlockStarts struct {
	values []markdownBlockStart
	limit  int
	failed bool
}

// markdownTrackedBlockParser 组合上游策略，在 Open 的真实源位置记录块起点。
type markdownTrackedBlockParser struct {
	strategy parser.BlockParser
	starts   *markdownBlockStarts
}

var _ parser.BlockParser = (*markdownTrackedBlockParser)(nil)

func (p *markdownTrackedBlockParser) Trigger() []byte { return p.strategy.Trigger() }
func (p *markdownTrackedBlockParser) CanInterruptParagraph() bool {
	return p.strategy.CanInterruptParagraph()
}
func (p *markdownTrackedBlockParser) CanAcceptIndentedLine() bool {
	return p.strategy.CanAcceptIndentedLine()
}
func (p *markdownTrackedBlockParser) Open(parent ast.Node, reader text.Reader, context parser.Context) (ast.Node, parser.State) {
	_, position := reader.Position()
	node, state := p.strategy.Open(parent, reader, context)
	if node != nil {
		n, capacity := len(p.starts.values), cap(p.starts.values)
		if n == capacity {
			next := max(8, capacity*2)
			if (capacity+next)*int(unsafe.Sizeof(markdownBlockStart{})) > p.starts.limit {
				p.starts.failed = true
				return node, state
			}
			values := make([]markdownBlockStart, n, next)
			copy(values, p.starts.values)
			p.starts.values = values
		}
		p.starts.values = append(p.starts.values, markdownBlockStart{node: node, offset: position.Start})
	}
	return node, state
}
func (p *markdownTrackedBlockParser) Continue(node ast.Node, reader text.Reader, context parser.Context) parser.State {
	state := p.strategy.Continue(node, reader, context)
	if _, fence := node.(*ast.FencedCodeBlock); fence && state == parser.Close {
		for i := len(p.starts.values) - 1; i >= 0; i-- {
			if p.starts.values[i].node == node {
				p.starts.values[i].closed = true
				break
			}
		}
	}
	return state
}
func (p *markdownTrackedBlockParser) Close(node ast.Node, reader text.Reader, context parser.Context) {
	p.strategy.Close(node, reader, context)
}
