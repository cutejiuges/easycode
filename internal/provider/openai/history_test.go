package openai

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
		testCommitHistory(t, &conversation.history, testInputEntry(t, NewUserItem(fmt.Sprintf("user-%d", index))), nativeHistoryEntry{
			Kind:    nativeHistorySample,
			Outputs: []NativeItem{{Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: fmt.Sprintf("assistant-%d", index)}}}},
		})
	}, conversation.ProjectHistory)
}

func TestProjectHistoryMatchesGoldenAndOmitsOpaqueData(t *testing.T) {
	conversation := &Conversation{}
	testCommitHistory(t, &conversation.history, testInputEntry(t, NewUserItem("hello\nworld")), nativeHistoryEntry{Kind: nativeHistorySample, Outputs: []NativeItem{
		{
			Type:             "reasoning",
			ID:               "reasoning-secret",
			Phase:            "analysis",
			ReasoningSummary: []ReasoningSummaryPart{{Type: "summary_text", Text: "private-summary"}},
			EncryptedContent: "opaque-encrypted-content",
		},
		{Type: "future_item", Raw: []byte(`{"type":"future_item","secret":"opaque-extension"}`)},
		{Type: "message", ID: "msg-secret", Role: "assistant", Phase: "final", Content: []ContentPart{{Type: "output_text", Text: "visible answer"}}},
	}})

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
	for _, opaque := range []string{"reasoning-secret", "analysis", "private-summary", "opaque-encrypted-content", "opaque-extension", "msg-secret", "final"} {
		if strings.Contains(string(first), opaque) {
			t.Fatalf("projection leaked %q: %s", opaque, first)
		}
	}
}
