package anthropic

import (
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/testsupport"
	"easycode/internal/secret"
)

func TestTaskCheckpointPreservesExactStateAndSource(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "claude-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	c := testConversation(t, instance)
	entries := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, newUserMessage("archived work")), nativeHistoryEntry{Kind: nativeHistorySample, Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeText, Text: "archived answer"}}}, Metadata: messageMetadata{ID: "answer", Model: "claude-test"}}})
	var envelopes []provider.NativeCommitEnvelope
	for _, e := range entries {
		c.history.Append(e)
		v, err := encodeNativeCommit(e)
		if err != nil {
			t.Fatal(err)
		}
		envelopes = append(envelopes, v)
	}
	input, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	testsupport.CheckTaskCheckpoint(t, c, input, entries[0].TurnID, testRestoreRecords(t, envelopes), func(value provider.CheckpointEnvelope) (domain.ContextSummary, error) {
		decoded, err := decodeCheckpoint(value)
		return decoded.Summary, err
	})
}
