package openai

import (
	"bytes"
	"testing"

	"easycode/internal/codec"
)

func TestNativeItemPreservesEncryptedReasoning(t *testing.T) {
	want := NativeItem{
		Type: "reasoning",
		ReasoningSummary: []ReasoningSummaryPart{{
			Type: "summary_text",
			Text: "summary",
		}},
		EncryptedContent: "opaque-encrypted-content",
	}
	encoded, err := codec.MarshalStable(want)
	if err != nil {
		t.Fatalf("marshal native item: %v", err)
	}

	var got NativeItem
	if err := codec.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("unmarshal native item: %v", err)
	}
	if got.EncryptedContent != want.EncryptedContent || len(got.ReasoningSummary) != 1 {
		t.Fatalf("native item changed: got %#v want %#v", got, want)
	}
}

func TestNativeItemReplaysUnknownExtensions(t *testing.T) {
	raw := []byte(`{"type":"future_item","id":"item-1","future":{"enabled":true}}`)
	var item NativeItem
	if err := codec.Unmarshal(raw, &item); err != nil {
		t.Fatalf("unmarshal native item: %v", err)
	}
	encoded, err := codec.MarshalStable(item)
	if err != nil {
		t.Fatalf("marshal native item: %v", err)
	}
	if !bytes.Equal(encoded, raw) {
		t.Fatalf("opaque item changed: got %s want %s", encoded, raw)
	}
}

func TestNewUserItemUsesResponsesInputText(t *testing.T) {
	encoded, err := codec.MarshalStable(NewUserItem("hello"))
	if err != nil {
		t.Fatalf("marshal user item: %v", err)
	}
	want := `{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}`
	if string(encoded) != want {
		t.Fatalf("user item: got %s want %s", encoded, want)
	}
}

func TestNativeTurnCloneDoesNotShareOpaqueData(t *testing.T) {
	turn := nativeTurn{
		User: NewUserItem("hello"),
		Outputs: []NativeItem{{
			Type:             "reasoning",
			ReasoningSummary: []ReasoningSummaryPart{{Type: "summary_text", Text: "private"}},
			EncryptedContent: "opaque-encrypted",
			Raw:              []byte(`{"type":"reasoning","encrypted_content":"opaque-encrypted"}`),
		}},
	}
	cloned := turn.clone()
	turn.User.Content[0].Text = "changed"
	turn.Outputs[0].ReasoningSummary[0].Text = "changed"
	turn.Outputs[0].Raw[0] = '['

	if cloned.User.Content[0].Text != "hello" || cloned.Outputs[0].ReasoningSummary[0].Text != "private" || cloned.Outputs[0].Raw[0] != '{' {
		t.Fatalf("native turn clone shares mutable data: %#v", cloned)
	}
}
