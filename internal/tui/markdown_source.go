package tui

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
	"unsafe"
)

type markdownSourceEscape struct{ start, end, original, next int }

// markdownSafeSource 在解析前净化控制字符，并只为发生变化的区间建立偏移映射。
func markdownSafeSourceBudget(source string, limit int) (string, []markdownSourceEscape, bool) {
	count, size := 0, len(source)
	for offset, r := range source {
		if markdownEscapesRune(source, offset, r) {
			count++
			size += 6 - utf8.RuneLen(r)
		}
	}
	if count == 0 {
		return source, nil, len(source) <= limit
	}
	if size > 256<<10 || size*2+count*int(unsafe.Sizeof(markdownSourceEscape{})) > limit {
		return "", nil, false
	}
	var out strings.Builder
	out.Grow(size)
	escapes := make([]markdownSourceEscape, 0, count)
	last := 0
	for offset, r := range source {
		if !markdownEscapesRune(source, offset, r) {
			continue
		}
		replacement := fmt.Sprintf("\\u%04x", r)
		if out.Len()+offset-last+len(replacement) > 256<<10 {
			return "", nil, false
		}
		out.WriteString(source[last:offset])
		start := out.Len()
		out.WriteString(replacement)
		_, size := utf8.DecodeRuneInString(source[offset:])
		last = offset + size
		escapes = append(escapes, markdownSourceEscape{start, out.Len(), offset, last})
	}
	if len(escapes) == 0 {
		return source, nil, true
	}
	if out.Len()+len(source)-last > 256<<10 {
		return "", nil, false
	}
	out.WriteString(source[last:])
	return out.String(), escapes, true
}
func (doc markdownDocument) originalOffset(offset int) int {
	index := sort.Search(len(doc.escapes), func(i int) bool { return doc.escapes[i].end > offset })
	if index < len(doc.escapes) && doc.escapes[index].start <= offset {
		return doc.escapes[index].original
	}
	if index == 0 {
		return offset
	}
	previous := doc.escapes[index-1]
	return offset + previous.next - previous.end
}

func markdownEscapesRune(source string, offset int, r rune) bool {
	return composerControl(r) && r != '\n' && r != '\t' && !(r == '\r' && offset+1 < len(source) && source[offset+1] == '\n')
}
