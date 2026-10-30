package openai

import (
	"encoding/json"
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/tool"
)

const maxResponsesRequestBytes = 16 << 20

// responsesRequest 是当前文本切片发送给 Responses API 的稳定请求。
type responsesRequest struct {
	Model   string         `json:"model"`
	Input   []NativeItem   `json:"input"`
	Tools   []responseTool `json:"tools"`
	Stream  bool           `json:"stream"`
	Store   bool           `json:"store"`
	Include []string       `json:"include"`
}

type responseTool struct {
	Type        string            `json:"type"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Parameters  json.RawMessage   `json:"parameters,omitempty"`
	Strict      bool              `json:"strict,omitempty"`
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
	itemCount := 0

	if projectInstructions != nil && projectInstructions.HasDocuments() {
		itemCount++
	}
	for _, entry := range history {
		itemCount += len(entry.UserInputs)
		itemCount += len(entry.Outputs) + len(entry.ToolOutputs)
	}
	input := make([]NativeItem, 0, itemCount)
	if projectInstructions != nil && projectInstructions.HasDocuments() {
		input = append(input, NewUserItem(projectInstructions.RenderedText()))
	}
	for _, entry := range history {
		for _, item := range entry.UserInputs {
			input = append(input, item.clone())
		}
		for _, item := range entry.Outputs {
			input = append(input, item.clone())
		}
		for _, item := range entry.ToolOutputs {
			input = append(input, item.clone())
		}
	}

	tools := make([]responseTool, 0, len(toolView.Facades()))
	for _, facade := range toolView.Facades() {
		switch facade.WireKind() {
		case tool.WireStructured:
			tools = append(tools, responseTool{Type: "function", Name: facade.Name(), Description: facade.Description(), Parameters: json.RawMessage(facade.InputSchema()), Strict: true})
		case tool.WireCustomFreeform:
			format := facade.FreeformFormat()
			tools = append(tools, responseTool{Type: "custom", Name: facade.Name(), Description: facade.Description(), Format: &customToolFormat{Type: format.Type(), Syntax: format.Syntax(), Definition: format.Definition()}})
		}
	}
	return responsesRequest{
		Model:   model,
		Input:   input,
		Tools:   tools,
		Stream:  true,
		Store:   false,
		Include: []string{"reasoning.encrypted_content"},
	}
}

func compileResponsesRequest(
	model string,
	history []nativeHistoryEntry,
	projectInstructions *domain.ProjectInstructionsSnapshot,
	toolView tool.FacadeView,
) (codec.CanonicalJSON, error) {
	if projectInstructions != nil {
		if err := projectInstructions.Validate(); err != nil {
			return codec.CanonicalJSON{}, fmt.Errorf("project instructions snapshot is invalid: %w", err)
		}
	}
	if toolView.Family() != domain.ProviderOpenAI || len(toolView.Facades()) != 7 {
		return codec.CanonicalJSON{}, fmt.Errorf("OpenAI tool catalog view is invalid")
	}
	return codec.MarshalCanonical(
		buildResponsesRequest(model, history, projectInstructions, toolView),
		maxResponsesRequestBytes,
	)
}
