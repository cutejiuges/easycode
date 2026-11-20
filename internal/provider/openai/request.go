package openai

import (
	"encoding/json"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

const maxResponsesRequestBytes = 16 << 20

type reasoningWire struct {
	Effort  domain.OpenAIEffort     `json:"effort,omitempty"`
	Summary domain.ReasoningSummary `json:"summary,omitempty"`
}

type responsesReasoningFields struct {
	Reasoning *reasoningWire `json:"reasoning,omitempty"`
}

type responsesInputFields struct {
	PromptCacheKey  string         `json:"prompt_cache_key,omitempty"`
	ToolChoice      string         `json:"tool_choice,omitempty"`
	MaxOutputTokens uint64         `json:"max_output_tokens,omitempty"`
	Model           string         `json:"model"`
	Input           []NativeItem   `json:"input"`
	Tools           []responseTool `json:"tools"`
}

type responsesStorageFields struct {
	Store   bool     `json:"store"`
	Include []string `json:"include"`
}

// 两种传输从相同字段组组装，同时保留各自已固定的 canonical 属性顺序。
type responsesRequest struct {
	responsesReasoningFields
	responsesInputFields
	Stream bool `json:"stream"`
	responsesStorageFields
}

type responseTool struct {
	Type        string            `json:"type"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Parameters  json.RawMessage   `json:"parameters,omitempty"`
	Strict      *bool             `json:"strict,omitempty"`
	Format      *customToolFormat `json:"format,omitempty"`
}

type customToolFormat struct {
	Type       string `json:"type"`
	Syntax     string `json:"syntax"`
	Definition string `json:"definition"`
}

func buildResponsesRequest(
	model string,
	history []nativeHistoryEntry,
	projectInstructions *domain.ProjectInstructionsSnapshot,
	toolView tool.FacadeView,
) responsesRequest {
	input := make([]NativeItem, 0, len(history))
	if projectInstructions != nil && projectInstructions.HasDocuments() {
		input = append(input, NewUserItem(projectInstructions.RenderedText()))
	}
	for _, entry := range history {
		for item := range nativeRequestItems(entry) {
			input = append(input, item.clone())
		}
	}

	facades := toolView.Facades()
	tools := make([]responseTool, 0, len(facades))
	for _, facade := range facades {
		switch facade.WireKind() {
		case tool.WireStructured:
			var strict *bool
			switch facade.ServerSchemaMode() {
			case tool.ServerSchemaStrict:
				value := true
				strict = &value
			case tool.ServerSchemaBestEffort:
				value := false
				strict = &value
			}
			tools = append(tools, responseTool{Type: "function", Name: facade.Name(), Description: facade.Description(), Parameters: json.RawMessage(facade.InputSchema()), Strict: strict})
		case tool.WireCustomFreeform:
			format := facade.FreeformFormat()
			tools = append(tools, responseTool{Type: "custom", Name: facade.Name(), Description: facade.Description(), Format: &customToolFormat{Type: format.Type(), Syntax: format.Syntax(), Definition: format.Definition()}})
		}
	}
	return responsesRequest{
		responsesInputFields: responsesInputFields{Model: model, Input: input, Tools: tools},
		Stream:               true,
		responsesStorageFields: responsesStorageFields{
			Include: []string{"reasoning.encrypted_content"},
		},
	}
}

func (request *responsesRequest) applyInference(settings domain.InferenceSettings) {
	request.MaxOutputTokens = settings.MaxOutputTokens()
	if settings.OpenAIEffort() != "" || settings.Summary() != "" {
		request.Reasoning = &reasoningWire{settings.OpenAIEffort(), settings.Summary()}
	}
}
