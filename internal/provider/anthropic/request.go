package anthropic

// messagesRequest 是当前文本切片发送给 Messages API 的稳定请求。
type messagesRequest struct {
	Model     string          `json:"model"`
	Messages  []nativeMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens"`
	Stream    bool            `json:"stream"`
}

func compileMessagesRequest(
	model string,
	maxTokens int,
	history []nativeTurn,
	user nativeMessage,
) messagesRequest {
	messages := make([]nativeMessage, 0, len(history)*2+1)
	for _, turn := range history {
		messages = append(messages, turn.User.clone(), turn.Assistant.clone())
	}
	messages = append(messages, user.clone())
	return messagesRequest{
		Model:     model,
		Messages:  messages,
		MaxTokens: maxTokens,
		Stream:    true,
	}
}
