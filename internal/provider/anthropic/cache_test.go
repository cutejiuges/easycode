package anthropic

import (
	"bytes"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"os"
	"testing"
)

func TestCacheRequestGoldens(t *testing.T) {
	history := []nativeHistoryEntry{testInputEntry(t, newUserMessage("hello"))}
	plan := samplingRequestPlan{request: buildMessagesRequest("claude-test", DefaultMaxOutputTokens, history, nil, testAnthropicToolView(t)), history: history}
	for _, item := range []struct {
		mode domain.AnthropicCacheMode
		path string
	}{{domain.AnthropicCache5m, "cache_5m_request.golden.json"}, {domain.AnthropicCache1h, "cache_1h_request.golden.json"}} {
		got, err := compileCachedMessagesRequest(plan, false, item.mode)
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile("testdata/" + item.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Bytes(), bytes.TrimSpace(want)) {
			t.Fatalf("cache golden differs %s:\n%s", item.path, got.Bytes())
		}
	}
}

func TestCacheOverlayRejectsUnselectedAndUnknownMarkerConflicts(t *testing.T) {
	for _, kind := range []string{"text", "future_block"} {
		var first NativeItem
		raw := []byte(`{"type":"` + kind + `","text":"preserve","cache_control":{"type":"ephemeral","ttl":"1h"}}`)
		if err := first.UnmarshalJSON(raw); err != nil {
			t.Fatal(err)
		}
		plan := samplingRequestPlan{request: messagesRequest{Messages: []nativeMessage{{Role: roleUser, Content: []NativeItem{first}}, {Role: roleUser, Content: []NativeItem{{Type: blockTypeText, Text: "selected tail"}}}}}}
		before, err := codec.MarshalStable(plan.request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compileCachedMessagesRequest(plan, false, domain.AnthropicCache5m); err == nil {
			t.Fatal("unselected native marker bypassed count or TTL policy")
		}
		after, err := codec.MarshalStable(plan.request)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("rejection modified native content")
		}
	}
}
