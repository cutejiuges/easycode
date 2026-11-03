package anthropic

import (
	"bytes"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/secret"
	"encoding/json"
	"os"
	"testing"
)

func TestConversationRejectsMissingThreadIdentity(t *testing.T) {
	instance, err := New(Config{Model: "fixture-model", BaseURL: "https://example.invalid", APIKey: secret.New("sentinel-secret")})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err = instance.NewConversation(""); err == nil {
		t.Fatal("missing identity accepted")
	}
	if _, err = instance.RestoreConversation("", nil); err == nil {
		t.Fatal("missing restored identity accepted")
	}
}

func TestCacheOverlayPreservesRawAndHistory(t *testing.T) {
	var raw NativeItem
	if err := raw.UnmarshalJSON([]byte(`{"type":"text","text":"answer","future":{"retained":true}}`)); err != nil {
		t.Fatal(err)
	}
	history := []nativeHistoryEntry{testInputEntry(t, newUserMessage("first")), {Kind: nativeHistorySample, Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeThinking, Thinking: "opaque-thinking", Signature: "opaque-signature"}, {Type: blockTypeRedactedThinking, RedactedData: "opaque-redacted"}, raw}}}, testInputEntry(t, newUserMessage("next"))}
	before, err := codec.MarshalStable(history)
	if err != nil {
		t.Fatal(err)
	}
	request := buildMessagesRequest("fixture-model", 4096, history, nil, testAnthropicToolView(t))
	plan := samplingRequestPlan{request: request, history: history}
	for _, mode := range []domain.AnthropicCacheMode{domain.AnthropicCacheDisabled, domain.AnthropicCache5m, domain.AnthropicCache1h} {
		compiled, err := compileCachedMessagesRequest(plan, false, mode)
		if err != nil {
			t.Fatal(err)
		}
		again, err := compileCachedMessagesRequest(plan, false, mode)
		if err != nil || !bytes.Equal(compiled.Bytes(), again.Bytes()) {
			t.Fatal("cache request unstable", err)
		}
		after, _ := codec.MarshalStable(history)
		if !bytes.Equal(before, after) {
			t.Fatal("overlay modified native history")
		}
		if !bytes.Contains(compiled.Bytes(), []byte(`"future":{"retained":true}`)) || !bytes.Contains(compiled.Bytes(), []byte("opaque-signature")) {
			t.Fatal("native content lost")
		}
		count := bytes.Count(compiled.Bytes(), []byte(`"cache_control"`))
		if mode == domain.AnthropicCacheDisabled {
			if count != 0 {
				t.Fatal("disabled cache has markers")
			}
		} else if count != 2 {
			t.Fatal("incorrect markers", count)
		}
		if (mode == domain.AnthropicCache1h) != bytes.Contains(compiled.Bytes(), []byte(`"ttl":"1h"`)) {
			t.Fatal("incorrect TTL")
		}
	}
}
func TestCacheHistoryBridgeAndSummarySuffix(t *testing.T) {
	for _, blocks := range []int{18, 19, 21} {
		content := make([]NativeItem, blocks)
		for i := range content {
			content[i] = NativeItem{Type: blockTypeText, Text: "block"}
		}
		history := []nativeHistoryEntry{testInputEntry(t, newUserMessage("first")), {Kind: nativeHistorySample, Assistant: nativeMessage{Role: roleAssistant, Content: content}}, testInputEntry(t, newUserMessage("next"))}
		r := buildMessagesRequest("fixture-model", 4096, history, nil, testAnthropicToolView(t))
		plan := samplingRequestPlan{request: r, history: history}
		positions := messageCachePositions(plan, false)
		want := 1
		if blocks >= 19 {
			want = 2
		}
		if len(positions) != want {
			t.Fatal("incorrect 20-block bridge", blocks, positions)
		}
		plan.request.Messages = append(plan.request.Messages, newUserMessage("one-off summary guidance"))
		summary, err := compileCachedMessagesRequest(plan, true, domain.AnthropicCache1h)
		if err != nil {
			t.Fatal(err)
		}
		members, err := codec.ObjectMembers(summary.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		var messages []struct {
			Content []json.RawMessage `json:"content"`
		}
		if err = codec.Unmarshal(members["messages"], &messages); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(messages[len(messages)-1].Content[0], []byte("cache_control")) {
			t.Fatal("one-off suffix cached")
		}
	}
}
func TestCacheOverlayRejectsNativeMarkerConflict(t *testing.T) {
	var item NativeItem
	if err := item.UnmarshalJSON([]byte(`{"type":"text","text":"original","cache_control":{"type":"ephemeral"}}`)); err != nil {
		t.Fatal(err)
	}
	plan := samplingRequestPlan{request: messagesRequest{Messages: []nativeMessage{{Role: roleUser, Content: []NativeItem{item}}}}}
	if _, err := compileCachedMessagesRequest(plan, false, domain.AnthropicCache5m); err == nil {
		t.Fatal("conflicting native marker overwritten")
	}
}

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

func TestCacheMarkerBudgetAndUnmarkableTail(t *testing.T) {
	content := make([]NativeItem, 21)
	for i := range content {
		content[i] = NativeItem{Type: blockTypeText, Text: "history"}
	}
	history := []nativeHistoryEntry{testInputEntry(t, newUserMessage("first")), {Kind: nativeHistorySample, Assistant: nativeMessage{Role: roleAssistant, Content: content}}, testInputEntry(t, newUserMessage("next"))}
	snapshot := testAnthropicProjectInstructions(t, "AGENTS.md", "Project instructions.")
	plan := samplingRequestPlan{request: buildMessagesRequest("fixture-model", 4096, history, &snapshot, testAnthropicToolView(t)), history: history, project: true}
	compiled, err := compileCachedMessagesRequest(plan, false, domain.AnthropicCache5m)
	if err != nil {
		t.Fatal(err)
	}
	count, err := requestMarkerCount(compiled, domain.AnthropicCache5m)
	if err != nil || count != 4 {
		t.Fatal("four-marker budget failed", count, err)
	}
	plan.request.Messages = []nativeMessage{{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeThinking, Thinking: "private", Signature: "signature"}, {Type: blockTypeRedactedThinking, RedactedData: "opaque"}, {Type: "future", Raw: json.RawMessage(`{"type":"future","data":"opaque"}`)}}}}
	plan.history = nil
	plan.project = false
	compiled, err = compileCachedMessagesRequest(plan, false, domain.AnthropicCache5m)
	if err != nil {
		t.Fatal(err)
	}
	count, err = requestMarkerCount(compiled, domain.AnthropicCache5m)
	if err != nil || count != 1 {
		t.Fatal("unmarkable blocks marked", count, err)
	}
	plan.request.Tools = nil
	compiled, err = compileCachedMessagesRequest(plan, false, domain.AnthropicCache5m)
	if err != nil {
		t.Fatal(err)
	}
	count, err = requestMarkerCount(compiled, domain.AnthropicCache5m)
	if err != nil || count != 0 {
		t.Fatal("empty eligible prefix marked", count, err)
	}
}

func TestCacheCapabilitiesFollowTTL(t *testing.T) {
	for _, mode := range []domain.AnthropicCacheMode{domain.AnthropicCacheDisabled, domain.AnthropicCache5m, domain.AnthropicCache1h} {
		policy, _ := domain.NewCachePolicy(mode, domain.OpenAICacheOmit, domain.OpenAIHTTPSSE)
		p, err := New(Config{Model: "model", BaseURL: "https://example.invalid", APIKey: secret.New("key"), CachePolicy: policy})
		if err != nil {
			t.Fatal(err)
		}
		caps := testConversation(t, p).Capabilities()
		if caps.PromptCacheControl != (mode != domain.AnthropicCacheDisabled) || caps.PromptCacheKey || caps.PreviousResponse || caps.ReasoningSummary || caps.ParallelToolCalls {
			t.Fatal(caps)
		}
		_ = p.Close()
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
