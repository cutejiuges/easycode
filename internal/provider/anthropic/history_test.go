package anthropic

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/provider/testsupport"
)

func TestProjectHistoryConcurrentCommitReturnsCompleteSnapshots(t *testing.T) {
	conversation := &Conversation{}
	testsupport.CheckConcurrentHistory(t, func(index int) {
		testCommitHistory(t, &conversation.history, testInputEntry(t, newUserMessage(fmt.Sprintf("user-%d", index))), nativeHistoryEntry{
			Kind:      nativeHistorySample,
			Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeText, Text: fmt.Sprintf("assistant-%d", index)}}},
		})
	}, conversation.ProjectHistory)
}

func TestProjectHistoryMatchesGoldenAndOmitsOpaqueData(t *testing.T) {
	conversation := &Conversation{}
	testCommitHistory(t, &conversation.history, testInputEntry(t, newUserMessage("hello\nworld")), nativeHistoryEntry{
		Kind: nativeHistorySample,
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{
			{Type: blockTypeThinking, Thinking: "private-thought", Signature: "opaque-signature"},
			{Type: blockTypeRedactedThinking, RedactedData: "opaque-data"},
			{Type: blockTypeText, Text: "visible answer"},
			{Type: "future_block", Raw: []byte(`{"type":"future_block","secret":"opaque-extension"}`)},
		}},
		Metadata: messageMetadata{ID: "msg-secret", Model: "claude-test", Usage: rawUsage{OutputTokens: optionalUint{Value: 9, Known: true}}},
	})

	first, err := codec.MarshalStable(conversation.ProjectHistory())
	if err != nil {
		t.Fatalf("marshal projection: %v", err)
	}
	second, err := codec.MarshalStable(conversation.ProjectHistory())
	if err != nil {
		t.Fatalf("marshal repeated projection: %v", err)
	}
	want, err := os.ReadFile("testdata/history_projection.golden.json")
	if err != nil {
		t.Fatalf("read projection golden: %v", err)
	}
	if string(first) != strings.TrimSpace(string(want)) || string(second) != string(first) {
		t.Fatalf("projection golden mismatch:\n got: %s\nwant: %s", first, want)
	}
	for _, opaque := range []string{"private-thought", "opaque-signature", "opaque-data", "opaque-extension", "msg-secret", "claude-test", "output_tokens"} {
		if strings.Contains(string(first), opaque) {
			t.Fatalf("projection leaked %q: %s", opaque, first)
		}
	}
}
