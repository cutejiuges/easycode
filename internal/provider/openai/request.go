package openai

// responsesRequest 是当前文本切片发送给 Responses API 的稳定请求。
type responsesRequest struct {
	Model   string       `json:"model"`
	Input   []NativeItem `json:"input"`
	Stream  bool         `json:"stream"`
	Store   bool         `json:"store"`
	Include []string     `json:"include"`
}

func compileResponsesRequest(model string, history []NativeItem, user NativeItem) responsesRequest {
	input := make([]NativeItem, 0, len(history)+1)
	for _, item := range history {
		input = append(input, item.clone())
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
