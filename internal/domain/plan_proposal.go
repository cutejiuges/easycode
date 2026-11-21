package domain

import (
	"strings"
	"unicode/utf8"
)

// MaxPlanProposalBytes 限制一份可审阅方案的正文大小。
const MaxPlanProposalBytes = 32 << 10

const (
	planOpenTag  = "<proposed_plan>"
	planCloseTag = "</proposed_plan>"
)

// ParsePlanProposal 提取独占最终答复的完整方案，保留标记行之间的原字节。
func ParsePlanProposal(text string) (string, bool) {
	start, end := -1, -1
	for offset := 0; offset < len(text); {
		line, remaining, newline := strings.Cut(text[offset:], "\n")
		next := len(text) - len(remaining)
		if newline {
			line = strings.TrimSuffix(line, "\r")
		}
		switch {
		case start < 0:
			if newline && line == planOpenTag {
				start = next
			} else if strings.TrimSpace(line) != "" {
				return "", false
			}
		case end >= 0:
			if strings.TrimSpace(line) != "" {
				return "", false
			}
		case line == planCloseTag:
			end = offset
		case line == planOpenTag || next-start > MaxPlanProposalBytes:
			return "", false
		}
		offset = next
	}
	if end < 0 {
		return "", false
	}
	markdown := text[start:end]
	if !utf8.ValidString(markdown) || strings.TrimSpace(markdown) == "" {
		return "", false
	}
	return strings.Clone(markdown), true
}

// PlanImplementationInput 将获批原文包装为显式实施输入。
func PlanImplementationInput(markdown string) string {
	return "Implement the following approved plan:\n\n" + markdown
}
