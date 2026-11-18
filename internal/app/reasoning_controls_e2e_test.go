package app

import (
	"bytes"
	"fmt"

	"easycode/internal/domain"
)

// activeReasoningFixture 复用工具循环事件，提供与可见正文独立的明确计量。
func activeReasoningFixture(family domain.ProviderFamily, sample int, tool bool) []byte {
	wire := reasoningFixtureStream(family, sample, tool)
	if family == domain.ProviderOpenAI {
		return bytes.ReplaceAll(wire, []byte(`"response":{"id":`), []byte(`"response":{"usage":{"input_tokens":100,"input_tokens_details":{"cached_tokens":0},"output_tokens":40,"output_tokens_details":{"reasoning_tokens":30}},"id":`))
	}
	stop := "end_turn"
	if tool && sample == 1 {
		stop = "tool_use"
	}
	delta := fmt.Sprintf("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":%q},\"usage\":{\"output_tokens\":40,\"output_tokens_details\":{\"thinking_tokens\":30}}}\n\n", stop)
	return bytes.Replace(wire, []byte(`data: {"type":"message_stop"}`), []byte(delta+`data: {"type":"message_stop"}`), 1)
}
