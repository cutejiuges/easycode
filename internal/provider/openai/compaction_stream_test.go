package openai

import (
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/testsupport"
	"easycode/internal/secret"
)

func TestCompactionStreamFailuresAndUTF8(t *testing.T) {
	testsupport.CheckCompactionStreams(t, compactionStreamFixture, compactionStreamFrames, []string{`"reasoning":{"effort":"high","summary":"detailed"}`, `"max_output_tokens":4096`})
}
func TestCompactionStreamCancellation(t *testing.T) {
	testsupport.CheckCompactionCancellation(t, compactionStreamFixture)
}
func compactionStreamFrames(mode string) []string {
	events := []string{
		`{"type":"response.created","response":{"id":"summary"}}`,
		`{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"修复 Error 后继续"}]}}`,
		`{"type":"response.completed","response":{"id":"summary","status":"completed"}}`,
	}
	switch mode {
	case "refusal":
		events[1] = `{"type":"response.refusal.delta","delta":"secret"}`
	case "incomplete":
		events[2] = `{"type":"response.completed","response":{"id":"summary","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`
	case "tool":
		events[1] = `{"type":"response.output_item.done","item":{"type":"function_call","id":"tool","call_id":"call","name":"exec_command","arguments":"{}"}}`
	case "empty":
		events[1] = `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":" "}]}}`
	case "disconnect":
		events = events[:2]
	}
	return events
}
func compactionStreamFixture(t *testing.T, url string) (testsupport.CompactionConversation, provider.CompactionInput) {
	t.Helper()
	settings, _ := domain.NewOpenAIInference("high", "detailed", 32768)
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
	for _, entry := range testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, NewUserItem(strings.Repeat("old history ", 100)))}) {
		conversation.history.Append(entry)
	}
	input, _ := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
	compact, _ := provider.NewCompactionInput(input, "", 4096)

	return conversation, compact
}
func (conversation *Conversation) TestOperationBusy() bool { return conversation.operations.Busy() }
