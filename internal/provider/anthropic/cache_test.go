package anthropic

import (
	"bytes"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"fmt"
	"os"
	"testing"
)

func TestCacheRequestGoldens(t *testing.T) {
	history := []nativeHistoryEntry{testInputEntry(t, newUserMessage("hello"))}
	plan := samplingRequestPlan{request: buildMessagesRequest("claude-test", DefaultMaxOutputTokens, history, nil, testAnthropicToolView(t))}
	for _, item := range []struct {
		mode domain.AnthropicCacheMode
		path string
	}{{domain.AnthropicCache5m, "cache_5m_request.golden.json"}, {domain.AnthropicCache1h, "cache_1h_request.golden.json"}} {
		got, err := plan.compile(item.mode)
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile("testdata/" + item.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.body.Bytes(), bytes.TrimSpace(want)) {
			t.Fatalf("cache golden differs %s:\n%s", item.path, got.body.Bytes())
		}
		if got.markers != 2 {
			t.Fatalf("wire and compiled marker count diverged: %d", got.markers)
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
		if _, err := plan.body(domain.AnthropicCache5m); err == nil {
			t.Fatal("unselected native marker bypassed count or TTL policy")
		}
		after, err := codec.MarshalStable(plan.request)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("rejection modified native content")
		}
	}
}

func requestMarkerCount(request codec.CanonicalJSON, mode domain.AnthropicCacheMode) (uint8, error) {
	if mode == domain.AnthropicCacheDisabled {
		return 0, nil
	}
	var wire struct {
		Messages []nativeMessage `json:"messages"`
		Tools    []struct {
			CacheControl *cacheControl `json:"cache_control"`
		} `json:"tools"`
	}
	if err := codec.Unmarshal(request.Bytes(), &wire); err != nil {
		return 0, err
	}
	count := 0
	for _, tool := range wire.Tools {
		if tool.CacheControl != nil {
			count++
		}
	}
	for _, message := range wire.Messages {
		for _, item := range message.Content {
			if !cacheable(item) {
				continue
			}
			members, err := codec.ObjectMembers(item.Raw)
			if err != nil {
				return 0, err
			}
			if _, exists := members["cache_control"]; exists {
				count++
			}
		}
	}
	if count > 4 {
		return 0, fmt.Errorf("cache marker limit exceeded")
	}
	return uint8(count), nil
}
