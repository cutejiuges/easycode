package openai

import (
	"testing"

	"easycode/internal/codec"
)

func TestNativeItemPreservesEncryptedReasoning(t *testing.T) {
	want := NativeItem{
		Type:             "reasoning",
		ReasoningSummary: []string{"summary"},
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
