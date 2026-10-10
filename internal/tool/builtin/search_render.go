package builtin

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"easycode/internal/tool"
)

const maxSearchLineCodePoints = 500

func renderSearchPreview(lines []string) (tool.ModelPreview, int) {
	rendered := make([]string, len(lines))
	prefixBytes := make([]int, len(lines)+1)
	for index, line := range lines {
		line, truncated := truncateSearchLine(line)
		if truncated {
			line += "... [line truncated]"
		}
		rendered[index] = line + "\n"
		prefixBytes[index+1] = prefixBytes[index] + len(rendered[index])
	}
	if prefixBytes[len(rendered)] <= tool.MaxSearchPreviewBytes {
		preview, _ := tool.NewModelPreview(strings.Join(rendered, ""))
		return preview, 0
	}
	bestKeep := 0
	for keep := 0; keep < len(rendered); keep++ {
		head := (keep + 1) / 2
		tail := keep / 2
		omitted := len(rendered) - keep
		marker := fmt.Sprintf("[... %d results omitted ...]\n", omitted)
		tailBytes := prefixBytes[len(rendered)] - prefixBytes[len(rendered)-tail]
		if prefixBytes[head]+len(marker)+tailBytes <= tool.MaxSearchPreviewBytes {
			bestKeep = keep
		}
	}
	head := (bestKeep + 1) / 2
	tail := bestKeep / 2
	omitted := len(rendered) - bestKeep
	var builder strings.Builder
	for _, line := range rendered[:head] {
		builder.WriteString(line)
	}
	fmt.Fprintf(&builder, "[... %d results omitted ...]\n", omitted)
	for _, line := range rendered[len(rendered)-tail:] {
		builder.WriteString(line)
	}
	preview, _ := tool.NewModelPreview(builder.String())
	return preview, omitted
}

func truncateSearchLine(value string) (string, bool) {
	if utf8.RuneCountInString(value) <= maxSearchLineCodePoints {
		return value, false
	}
	count := 0
	for index := range value {
		if count == maxSearchLineCodePoints {
			return value[:index], true
		}
		count++
	}
	return value, false
}
