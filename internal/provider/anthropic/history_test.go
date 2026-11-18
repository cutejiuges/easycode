package anthropic

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func TestProjectHistoryConcurrentCommitReturnsCompleteSnapshots(t *testing.T) {
	conversation := &Conversation{}
	start := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-start
		for index := 0; index < 200; index++ {
			testCommitHistory(t, &conversation.history, testInputEntry(t, newUserMessage(fmt.Sprintf("user-%d", index))), nativeHistoryEntry{
				Kind:      nativeHistorySample,
				Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeText, Text: fmt.Sprintf("assistant-%d", index)}}},
			})
			runtime.Gosched()
		}
	}()
	close(start)

	for {
		projection := conversation.ProjectHistory()
		assertCompleteAnthropicProjection(t, projection)
		select {
		case <-done:
			final := conversation.ProjectHistory()
			assertCompleteAnthropicProjection(t, final)
			if len(final.Turns) != 200 {
				t.Fatalf("final turn count: %d", len(final.Turns))
			}
			return
		default:
			runtime.Gosched()
		}
	}
}

func assertCompleteAnthropicProjection(t *testing.T, projection domain.SemanticHistoryView) {
	t.Helper()
	for index, turn := range projection.Turns {
		wantUser := fmt.Sprintf("user-%d", index)
		wantAssistant := fmt.Sprintf("assistant-%d", index)
		if len(turn.Messages) == 0 || len(turn.Messages) > 2 || turn.Messages[0].Text != wantUser || (len(turn.Messages) == 2 && turn.Messages[1].Text != wantAssistant) || (len(turn.Messages) == 1 && index != len(projection.Turns)-1) {
			t.Fatalf("partial turn %d: %#v", index, turn)
		}
	}
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
