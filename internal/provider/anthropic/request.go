package anthropic

import (
	"encoding/json"
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/tool"
)

const maxMessagesRequestBytes = 16 << 20

// messagesRequest 是当前文本切片发送给 Messages API 的稳定请求。
type messagesRequest struct {
	Model     string          `json:"model"`
	Messages  []nativeMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens"`
	Tools     []anthropicTool `json:"tools"`
	Stream    bool            `json:"stream"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

func buildMessagesRequest(
	model string,
	maxTokens int,
	history []nativeHistoryEntry,
	projectInstructions *domain.ProjectInstructionsSnapshot,
	currentInput *nativeMessage,
	toolView tool.FacadeView,
) messagesRequest {
	messageCount := len(history)*2 + 1
	if currentInput == nil {
		messageCount--
	}
	if projectInstructions != nil && projectInstructions.HasDocuments() {
		messageCount++
	}
	messages := make([]nativeMessage, 0, messageCount)
	if projectInstructions != nil && projectInstructions.HasDocuments() {
		messages = append(messages, newUserMessage(projectInstructions.RenderedText()))
	}
	for _, entry := range history {
		if entry.Input != nil {
			messages = append(messages, entry.Input.clone())
		}
		if entry.Kind == nativeHistorySample {
			messages = append(messages, entry.Assistant.clone())
		} else {
			messages = append(messages, entry.ToolOutputs.clone())
		}
	}
	if currentInput != nil {
		messages = append(messages, currentInput.clone())
	}
	tools := make([]anthropicTool, 0, len(toolView.Facades()))
	for _, facade := range toolView.Facades() {
		tools = append(tools, anthropicTool{
			Name: facade.Name(), Description: facade.Description(), InputSchema: json.RawMessage(facade.InputSchema()),
		})
	}
	return messagesRequest{
		Model:     model,
		Messages:  messages,
		MaxTokens: maxTokens,
		Tools:     tools,
		Stream:    true,
	}
}

func compileMessagesRequest(
	model string,
	maxTokens int,
	history []nativeHistoryEntry,
	projectInstructions *domain.ProjectInstructionsSnapshot,
	currentInput *nativeMessage,
	toolView tool.FacadeView,
) (codec.CanonicalJSON, error) {
	if projectInstructions != nil {
		if err := projectInstructions.Validate(); err != nil {
			return codec.CanonicalJSON{}, fmt.Errorf("project instructions snapshot is invalid: %w", err)
		}
	}
	if toolView.Family() != domain.ProviderAnthropic || len(toolView.Facades()) != 1 {
		return codec.CanonicalJSON{}, fmt.Errorf("anthropic tool catalog view is invalid")
	}
	return codec.MarshalCanonical(
		buildMessagesRequest(model, maxTokens, history, projectInstructions, currentInput, toolView),
		maxMessagesRequestBytes,
	)
}
