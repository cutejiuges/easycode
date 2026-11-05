package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const markdownNodeLimit = 32768
const markdownMemoryLimit = 2 << 20

type markdownCodeCache struct {
	items []markdownCode
	scans int
}
type markdownCode struct {
	node   ast.Node
	result highlightResult
}

type markdownDocument struct {
	blocks    []markdownBlockStart
	escapes   []markdownSourceEscape
	plainCode ast.Node
	codes     *markdownCodeCache
	source    []byte
	root      ast.Node
	nodes     int
	failure   bool
}

// markdownParser 只持有实例解析配置，不调用全局 Convert 或 HTML renderer。
type markdownParser struct {
	starts          *markdownBlockStarts
	parser          parser.Parser
	parses, scanned int
	availableBytes  int
}

func newMarkdownParser() markdownParser {
	starts := &markdownBlockStarts{}
	blocks := parser.DefaultBlockParsers()
	for i := range blocks {
		blocks[i].Value = &markdownTrackedBlockParser{blocks[i].Value.(parser.BlockParser), starts}
	}
	return markdownParser{availableBytes: markdownMemoryLimit, starts: starts, parser: parser.NewParser(
		parser.WithBlockParsers(blocks...),
		parser.WithInlineParsers(append(parser.DefaultInlineParsers(), util.Prioritized(extension.NewStrikethroughParser(), 500), util.Prioritized(extension.NewTaskCheckBoxParser(), 0))...),
		parser.WithParagraphTransformers(append(parser.DefaultParagraphTransformers(), util.Prioritized(extension.NewTableParagraphTransformer(), 200))...),
		parser.WithASTTransformers(util.Prioritized(extension.NewTableASTTransformer(), 0)),
	)}
}

// markdownNodeReservation 对会生成节点的标记和补齐表格单元格先做保守预留。
func markdownNodeReservation(source string) int {
	nodes, lines, pipes, maxPipes := 16, 1, 0, 0
	for _, c := range source {
		switch c {
		case '\n':
			lines++
			maxPipes = max(maxPipes, pipes)
			pipes = 0
		case '|':
			pipes++
			nodes += 4
		case '*', '_', '~', '`', '[', ']', '!', '<', '>', '#', '-', '+', '\\':
			nodes += 4
		}
		if nodes > markdownNodeLimit {
			return nodes
		}
	}
	maxPipes = max(maxPipes, pipes)
	return nodes + 4*lines + lines*maxPipes
}
func (p *markdownParser) parse(source string, available int) markdownDocument {
	if len(source) > 256<<10 || !utf8.ValidString(source) {
		return markdownDocument{failure: true}
	}
	safe, escapes, ok := markdownSafeSourceBudget(source, p.availableBytes)
	if !ok {
		return markdownDocument{failure: true}
	}
	reservation := markdownNodeReservation(safe)
	if reservation > available {
		return markdownDocument{failure: true}
	}
	p.parses++
	p.scanned += len(source)
	sourceBytes := 2*len(safe) + cap(escapes)*32
	if sourceBytes > p.availableBytes {
		return markdownDocument{failure: true}
	}
	data := []byte(safe)
	p.starts.values = nil
	p.starts.limit = p.availableBytes - sourceBytes
	p.starts.failed = false
	root := p.parser.Parse(text.NewReader(data))
	blocks := p.starts.values
	p.starts.values = nil
	count := 0
	_ = ast.Walk(root, func(_ ast.Node, enter bool) (ast.WalkStatus, error) {
		if enter {
			count++
		}
		return ast.WalkContinue, nil
	})
	// 后置检查防止上游节点分配策略改变后继续发布超额派生状态。
	if count > reservation || p.starts.failed {
		return markdownDocument{failure: true}
	}
	return markdownDocument{source: data, root: root, nodes: count, codes: &markdownCodeCache{}, escapes: escapes, blocks: blocks}
}
func markdownBlockText(node ast.Node, source []byte) string {
	var out strings.Builder
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		segment := lines.At(i)
		out.Write(segment.Value(source))
	}
	return out.String()
}
