package openai

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

const maxResponsesRequestBytes = 16 << 20

// responsesRequest 是当前文本切片发送给 Responses API 的稳定请求。
type responsesRequest struct {
	Model   string       `json:"model"`
	Input   []NativeItem `json:"input"`
	Stream  bool         `json:"stream"`
	Store   bool         `json:"store"`
	Include []string     `json:"include"`
}

func buildResponsesRequest(
	model string,
	history []nativeTurn,
	projectInstructions *domain.ProjectInstructionsSnapshot,
	user NativeItem,
) responsesRequest {
	itemCount := 1
	if projectInstructions != nil && projectInstructions.HasDocuments() {
		itemCount++
	}
	for _, turn := range history {
		itemCount += 1 + len(turn.Outputs)
	}
	input := make([]NativeItem, 0, itemCount)
	if projectInstructions != nil && projectInstructions.HasDocuments() {
		input = append(input, NewUserItem(projectInstructions.RenderedText()))
	}
	for _, turn := range history {
		input = append(input, turn.User.clone())
		for _, item := range turn.Outputs {
			input = append(input, item.clone())
		}
	}
	input = append(input, user.clone())
	return responsesRequest{
		Model:   model,
		Input:   input,
		Stream:  true,
		Store:   false,
		Include: []string{"reasoning.encrypted_content"},
	}
}

func compileResponsesRequest(
	model string,
	history []nativeTurn,
	projectInstructions *domain.ProjectInstructionsSnapshot,
	user NativeItem,
) (codec.CanonicalJSON, error) {
	if projectInstructions != nil {
		if err := projectInstructions.Validate(); err != nil {
			return codec.CanonicalJSON{}, fmt.Errorf("project instructions snapshot is invalid: %w", err)
		}
	}
	return codec.MarshalCanonical(
		buildResponsesRequest(model, history, projectInstructions, user),
		maxResponsesRequestBytes,
	)
}
