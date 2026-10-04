package anthropic

import (
	"bytes"
	"testing"

	"easycode/internal/codec"
)

func TestNativeItemPreservesThinkingSignature(t *testing.T) {
	want := NativeItem{Type: blockTypeThinking, Thinking: "summary", Signature: "opaque-signature"}
	encoded, err := codec.MarshalStable(want)
	if err != nil {
		t.Fatalf("marshal native item: %v", err)
	}

	var got NativeItem
	if err := codec.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("unmarshal native item: %v", err)
	}
	if got.Signature != want.Signature || got.Thinking != want.Thinking {
		t.Fatalf("native item changed: got %#v want %#v", got, want)
	}
}

func TestNativeItemReplaysOpaqueRedactedThinking(t *testing.T) {
	raw := []byte(`{"type":"redacted_thinking","data":"opaque-data","future":{"enabled":true}}`)
	var item NativeItem
	if err := codec.Unmarshal(raw, &item); err != nil {
		t.Fatalf("unmarshal native item: %v", err)
	}
	if item.Type != blockTypeRedactedThinking || item.RedactedData != "opaque-data" {
		t.Fatalf("decoded item: %#v", item)
	}
	encoded, err := codec.MarshalStable(item)
	if err != nil {
		t.Fatalf("marshal native item: %v", err)
	}
	if !bytes.Equal(encoded, raw) {
		t.Fatalf("opaque item changed: got %s want %s", encoded, raw)
	}
}

func TestNativeItemKeepsRequiredEmptyFields(t *testing.T) {
	tests := []struct {
		name string
		item NativeItem
		want string
	}{
		{name: "text", item: NativeItem{Type: blockTypeText}, want: `{"type":"text","text":""}`},
		{name: "thinking", item: NativeItem{Type: blockTypeThinking}, want: `{"type":"thinking","thinking":"","signature":""}`},
		{name: "redacted", item: NativeItem{Type: blockTypeRedactedThinking}, want: `{"type":"redacted_thinking","data":""}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := codec.MarshalStable(test.item)
			if err != nil {
				t.Fatalf("marshal native item: %v", err)
			}
			if string(encoded) != test.want {
				t.Fatalf("native item: got %s want %s", encoded, test.want)
			}
		})
	}
}

func TestNativeTurnCloneDoesNotShareOpaqueData(t *testing.T) {
	turn := nativeTurn{
		User: newUserMessage("hello"),
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{
			Type: blockTypeRedactedThinking,
			Raw:  []byte(`{"type":"redacted_thinking","data":"opaque"}`),
		}}},
		Metadata: messageMetadata{Usage: rawUsage{
			InputTokens: optionalUint{Value: 3, Known: true},
		}},
	}
	cloned := turn.clone()
	turn.Assistant.Content[0].Raw[0] = '['
	turn.Assistant.Content[0].RedactedData = "changed"

	if cloned.Assistant.Content[0].Raw[0] != '{' {
		t.Fatalf("clone shares raw bytes: %s", cloned.Assistant.Content[0].Raw)
	}
	if !cloned.Metadata.Usage.InputTokens.Known || cloned.Metadata.Usage.InputTokens.Value != 3 {
		t.Fatalf("clone lost usage: %#v", cloned.Metadata.Usage)
	}
	if cloned.Metadata.Usage.CacheReadInputTokens.Known {
		t.Fatalf("missing usage became known: %#v", cloned.Metadata.Usage)
	}
}
