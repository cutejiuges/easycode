package anthropic

import (
	"encoding/json"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

const maxMessagesRequestBytes = 16 << 20

// messagesRequest 是当前文本切片发送给 Messages API 的稳定请求。
type thinkingWire struct {
	Type    domain.ThinkingMode    `json:"type"`
	Budget  uint64                 `json:"budget_tokens,omitempty"`
	Display domain.ThinkingDisplay `json:"display,omitempty"`
}

type outputConfigWire struct {
	Effort domain.AnthropicEffort `json:"effort"`
}

type messagesRequest struct {
	Thinking          *thinkingWire      `json:"thinking,omitempty"`
	OutputConfig      *outputConfigWire  `json:"output_config,omitempty"`
	ToolChoice        *summaryToolChoice `json:"tool_choice,omitempty"`
	Model             string             `json:"model"`
	Messages          []nativeMessage    `json:"messages"`
	MaxTokens         int                `json:"max_tokens"`
	Tools             []anthropicTool    `json:"tools"`
	Stream            bool               `json:"stream"`
	lastSampleMessage int
}

type summaryToolChoice struct {
	Type string `json:"type"`
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
	toolView tool.FacadeView,
) messagesRequest {
	messageCount := len(history)*2 + 1

	if projectInstructions != nil && projectInstructions.HasDocuments() {
		messageCount++
	}
	messages := make([]nativeMessage, 0, messageCount)
	if projectInstructions != nil && projectInstructions.HasDocuments() {
		messages = append(messages, newUserMessage(projectInstructions.RenderedText()))
	}
	lastSampleMessage := -1
	for _, entry := range history {
		for message := range nativeRequestMessages(entry) {
			messages = append(messages, message.clone())
		}
		if entry.Kind == nativeHistorySample {
			lastSampleMessage = len(messages) - 1
		}
	}

	facades := toolView.Facades()
	tools := make([]anthropicTool, 0, len(facades))
	for _, facade := range facades {
		tools = append(tools, anthropicTool{
			Name: facade.Name(), Description: facade.Description(), InputSchema: json.RawMessage(facade.InputSchema()),
		})
	}
	return messagesRequest{
		Model:             model,
		Messages:          messages,
		MaxTokens:         maxTokens,
		Tools:             tools,
		Stream:            true,
		lastSampleMessage: lastSampleMessage,
	}
}

func (request *messagesRequest) applyInference(settings domain.InferenceSettings) {
	request.MaxTokens = int(settings.MaxOutputTokens())
	t := settings.Thinking()
	if t.Mode() != domain.ThinkingOmit {
		request.Thinking = &thinkingWire{t.Mode(), t.BudgetTokens(), t.Display()}
	}
	if settings.AnthropicEffort() != "" {
		request.OutputConfig = &outputConfigWire{settings.AnthropicEffort()}
	}
}
