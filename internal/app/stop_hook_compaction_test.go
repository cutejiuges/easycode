package app

import (
	"fmt"
	"io"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func serveStopAssistant(w io.Writer, family domain.ProviderFamily, text string) {
	encoded, _ := codec.MarshalStable(text)
	if family == domain.ProviderOpenAI {
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"stop-response\"}}\n\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":%s}]}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"stop-response\"}}\n\n", encoded)
	} else {
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"stop-message\",\"model\":\"fixture-model\"}}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n", encoded)
	}
}
