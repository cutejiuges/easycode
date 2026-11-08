package openai

import (
	"easycode/internal/domain"
	"errors"
)

// reasoningBody 只提取明确的摘要段，原生密文和 raw content 始终不展示。
func reasoningBody(item NativeItem) (domain.ReasoningBody, error) {
	if len(item.ReasoningSummary) > domain.MaxReasoningNodes {
		return domain.ReasoningBody{}, errors.New("reasoning summary exceeds part limit")
	}
	parts := make([]string, 0, len(item.ReasoningSummary))
	for _, part := range item.ReasoningSummary {
		if part.Type == "summary_text" && part.Text != "" {
			parts = append(parts, part.Text)
		}
	}
	return domain.NewReasoningBody(domain.ReasoningOpenAISummary, parts)
}
