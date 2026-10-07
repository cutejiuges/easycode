package anthropic

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

const maxMessagesRequestBytes = 16 << 20

// messagesRequest 是当前文本切片发送给 Messages API 的稳定请求。
type messagesRequest struct {
	Model     string          `json:"model"`
	Messages  []nativeMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens"`
	Stream    bool            `json:"stream"`
}

func buildMessagesRequest(
	model string,
	maxTokens int,
	history []nativeTurn,
	projectInstructions *domain.ProjectInstructionsSnapshot,
	user nativeMessage,
) messagesRequest {
	messageCount := len(history)*2 + 1
	if projectInstructions != nil && projectInstructions.HasDocuments() {
		messageCount++
	}
	messages := make([]nativeMessage, 0, messageCount)
	if projectInstructions != nil && projectInstructions.HasDocuments() {
		messages = append(messages, newUserMessage(projectInstructions.RenderedText()))
	}
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

func compileMessagesRequest(
	model string,
	maxTokens int,
	history []nativeTurn,
	projectInstructions *domain.ProjectInstructionsSnapshot,
	user nativeMessage,
) (codec.CanonicalJSON, error) {
	if projectInstructions != nil {
		if err := projectInstructions.Validate(); err != nil {
			return codec.CanonicalJSON{}, fmt.Errorf("project instructions snapshot is invalid: %w", err)
		}
	}
	return codec.MarshalCanonical(
		buildMessagesRequest(model, maxTokens, history, projectInstructions, user),
		maxMessagesRequestBytes,
	)
}
