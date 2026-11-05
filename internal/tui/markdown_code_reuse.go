package tui

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
)

// markdownCodeCursor 顺序遍历旧文档已扫描的围栏，不复制 token 或正文。
type markdownCodeCursor struct {
	stream         *markdownStream
	fragment, item int
}

func (s *markdownStream) document(index int) (markdownDocument, int) {
	if index < len(s.fragments) {
		f := s.fragments[index]
		return f.document, f.offset
	}
	return s.tail, s.stable
}
func (c *markdownCodeCursor) next() (markdownDocument, markdownCode, int, bool) {
	for c.fragment <= len(c.stream.fragments) {
		doc, base := c.stream.document(c.fragment)
		if doc.codes != nil && c.item < len(doc.codes.items) {
			code := doc.codes.items[c.item]
			fence := code.node.(*ast.FencedCodeBlock)
			return doc, code, base + doc.originalOffset(fence.Info.Segment.Start), true
		}
		c.fragment++
		c.item = 0
	}
	return markdownDocument{}, markdownCode{}, 0, false
}

// 只在同一来源的递增正文内移交结果，同时核对原位置、语言和各行的实际字节。
func (s *markdownStream) reuseCodes(old *markdownStream) {
	cursor := markdownCodeCursor{stream: old}
	if _, _, _, ok := cursor.next(); !ok {
		return
	}
	for i := 0; i <= len(s.fragments); i++ {
		doc, base := s.document(i)
		if doc.root == nil || doc.codes == nil || len(doc.codes.items) > 0 {
			continue
		}
		_ = ast.Walk(doc.root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
			next, ok := node.(*ast.FencedCodeBlock)
			if !entering || !ok || next.Info == nil {
				return ast.WalkContinue, nil
			}
			position := base + doc.originalOffset(next.Info.Segment.Start)
			for {
				previous, code, offset, exists := cursor.next()
				if !exists {
					return ast.WalkStop, nil
				}
				if offset > position {
					break
				}
				cursor.item++
				if offset == position {
					fence := code.node.(*ast.FencedCodeBlock)
					if codeLanguage(string(fence.Language(previous.source))) == codeLanguage(string(next.Language(doc.source))) && sameMarkdownCodeLines(previous, fence, doc, next) {
						doc.codes.items = append(doc.codes.items, markdownCode{next, code.result})
					}
					break
				}
			}
			return ast.WalkContinue, nil
		})
	}
}
func sameMarkdownCodeLines(left markdownDocument, a ast.Node, right markdownDocument, b ast.Node) bool {
	if a.Lines().Len() != b.Lines().Len() {
		return false
	}
	for i := 0; i < a.Lines().Len(); i++ {
		x, y := a.Lines().At(i), b.Lines().At(i)
		if x.Padding != y.Padding || !bytes.Equal(left.source[x.Start:x.Stop], right.source[y.Start:y.Stop]) {
			return false
		}
	}
	return true
}
