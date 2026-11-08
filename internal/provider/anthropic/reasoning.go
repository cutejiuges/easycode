package anthropic

import "easycode/internal/domain"

// reasoningBody 只提取允许文字，签名和遮蔽数据不进入展示值。
func reasoningBody(item NativeItem) (domain.ReasoningBody, error) {
	if item.Type == blockTypeThinking {
		return domain.NewReasoningBody(domain.ReasoningAnthropicThinking, []string{item.Thinking})
	}
	return domain.NewReasoningBody(domain.ReasoningAnthropicThinking, nil)
}
