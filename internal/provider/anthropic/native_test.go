package anthropic

import (
	"testing"

	"easycode/internal/codec"
)

func TestNativeItemPreservesThinkingSignature(t *testing.T) {
	want := NativeItem{Type: "thinking", Thinking: "summary", Signature: "opaque-signature"}
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
