package anthropic

import (
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/testsupport"
	"easycode/internal/secret"
)

func TestCompactionStreamFailuresAndUTF8(t *testing.T) {
	testsupport.CheckCompactionStreams(t, compactionStreamFixture, compactionStreamFrames, []string{`"thinking":{"type":"adaptive","display":"omitted"}`, `"output_config":{"effort":"high"}`, `"max_tokens":4096`})
}
func TestCompactionStreamCancellation(t *testing.T) {
	testsupport.CheckCompactionCancellation(t, compactionStreamFixture)
}
func compactionStreamFrames(mode string) []string {
	events := []string{
		`{"type":"message_start","message":{"id":"summary","type":"message","role":"assistant","model":"model","content":[],"usage":{"input_tokens":100,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"修复 Error 后继续"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}`,
		`{"type":"message_stop"}`,
	}
	switch mode {
	case "refusal":
		events[4] = `{"type":"message_delta","delta":{"stop_reason":"refusal"}}`
	case "incomplete":
		events[4] = `{"type":"message_delta","delta":{"stop_reason":"max_tokens"}}`
	case "tool":
		events[1] = `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call","name":"exec_command","input":{}}}`
	case "empty":
		events[2] = `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" "}}`
	case "disconnect":
		events = events[:4]
	}
	return events
}
func compactionStreamFixture(t *testing.T, url string) (testsupport.CompactionConversation, provider.CompactionInput) {
	t.Helper()
	thinking, _ := domain.NewThinkingSettings(domain.ThinkingAdaptive, 0, domain.DisplayOmitted)
	settings, _ := domain.NewAnthropicInference(thinking, "high", 16384)
	instance, err := New(Config{Inference: settings, BaseURL: url, APIKey: secret.New("private-secret"), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := instance.Close(); err != nil {
			t.Error(err)
		}
	})
	conversation := testConversation(t, instance)
	for _, entry := range testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, newUserMessage(strings.Repeat("old history ", 100)))}) {
		conversation.history.Append(entry)
	}
	input, _ := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
	compact, _ := provider.NewCompactionInput(input, "", 4096)

	return conversation, compact
}
func (conversation *Conversation) TestOperationBusy() bool { return conversation.operations.Busy() }
