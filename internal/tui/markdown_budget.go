package tui

import "unsafe"

type markdownCost struct{ nodes, bytes int }

func (cost *markdownCost) add(other markdownCost) {
	cost.nodes += other.nodes
	cost.bytes += other.bytes
}
func markdownDocumentCost(doc markdownDocument) markdownCost {
	cost := markdownCost{nodes: doc.nodes, bytes: cap(doc.blocks)*int(unsafe.Sizeof(markdownBlockStart{})) + cap(doc.source) + cap(doc.escapes)*int(unsafe.Sizeof(markdownSourceEscape{}))}
	if doc.codes != nil {
		cost.bytes += cap(doc.codes.items) * int(unsafe.Sizeof(markdownCode{}))
		for _, code := range doc.codes.items {
			cost.nodes += len(code.result.tokens)
			cost.bytes += cap(code.result.tokens) * int(unsafe.Sizeof(highlightToken{}))
		}
	}
	return cost
}
func markdownLayoutCost(layout markdownLayout) int {
	size := cap(layout.rows) * int(unsafe.Sizeof(markdownRow{}))
	for _, row := range layout.rows {
		size += len(row.text)
	}
	return size
}
func (s markdownStream) cost() markdownCost {
	cost := markdownDocumentCost(s.tail)
	cost.bytes += cap(s.fragments) * int(unsafe.Sizeof(markdownFragment{}))
	for _, fragment := range s.fragments {
		cost.add(markdownDocumentCost(fragment.document))
	}
	return cost
}
func (c markdownViewCache) cost() markdownCost {
	cost := markdownCost{bytes: c.externalBytes + cap(c.lines)*16 + cap(c.entries)*int(unsafe.Sizeof(markdownViewEntry{}))}
	for _, line := range c.lines {
		cost.bytes += len(line)
	}
	for _, entry := range c.entries {
		cost.add(entry.stream.cost())
		cost.bytes += markdownLayoutCost(entry.layout)
	}
	return cost
}
