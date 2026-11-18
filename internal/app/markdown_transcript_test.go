package app

import (
	"bytes"
	"easycode/internal/domain"
	"fmt"
	"testing"
)

func addMarkdownReasoningFixture(wire []byte, family domain.ProviderFamily, turn int) []byte {
	ending := `data: {"type":"message_stop"`
	frames := fmt.Sprintf("data: %s\n\ndata: %s\n\n", fmt.Sprintf(`{"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"private-opaque-%d"}}`, turn), `{"type":"content_block_stop","index":1}`)
	if family == domain.ProviderOpenAI {
		ending = `data: {"type":"response.completed"`
		frames = fmt.Sprintf("data: %s\n\n", fmt.Sprintf(`{"type":"response.output_item.done","item":{"type":"reasoning","id":"rs-%d","summary":[],"encrypted_content":"private-opaque-%d"}}`, turn, turn))
	}
	return bytes.Replace(wire, []byte(ending), []byte(frames+ending), 1)
}
func assertMarkdownHistoryOrder(t *testing.T, body []byte) {
	t.Helper()
	for _, value := range []string{"first", "answer-1", "private-opaque-1", "second", "answer-2", "private-opaque-2", "third"} {
		marker := []byte(fmt.Sprintf("%q", value))
		index := bytes.Index(body, marker)
		if index < 0 {
			t.Fatalf("missing native content or wrong order at %s", value)
		}
		body = body[index+len(marker):]
	}
}
