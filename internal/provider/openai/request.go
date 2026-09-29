package openai

import "easycode/internal/codec"

const maxResponsesRequestBytes = 16 << 20

// responsesRequest 是当前文本切片发送给 Responses API 的稳定请求。
type responsesRequest struct {
	Model   string       `json:"model"`
	Input   []NativeItem `json:"input"`
	Stream  bool         `json:"stream"`
	Store   bool         `json:"store"`
	Include []string     `json:"include"`
}

func buildResponsesRequest(model string, history []nativeTurn, user NativeItem) responsesRequest {
	itemCount := 1
	for _, turn := range history {
		itemCount += 1 + len(turn.Outputs)
	}
	input := make([]NativeItem, 0, itemCount)
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

func compileResponsesRequest(model string, history []nativeTurn, user NativeItem) (codec.CanonicalJSON, error) {
	return codec.MarshalCanonical(buildResponsesRequest(model, history, user), maxResponsesRequestBytes)
}
