package protocol

import (
	"testing"

	"easycode/internal/codec"
)

func TestAssistantTextDeltaPayloadRoundTrip(t *testing.T) {
	event, err := NewAssistantTextDelta("hello")
	if err != nil {
		t.Fatalf("new text delta: %v", err)
	}
	payload, err := DecodeAssistantTextDelta(event)
	if err != nil {
		t.Fatalf("decode text delta: %v", err)
	}
	if payload.Text != "hello" || event.Version != CurrentVersion {
		t.Fatalf("unexpected event: %#v payload=%#v", event, payload)
	}
	encoded, err := codec.MarshalStable(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if string(encoded) != `{"text":"hello"}` {
		t.Fatalf("payload JSON: %s", encoded)
	}
}

func TestAssistantTextDeltaRejectsMissingText(t *testing.T) {
	if _, err := NewAssistantTextDelta(""); err == nil {
		t.Fatal("expected missing text error")
	}
}

func TestTurnFailedPayloadRoundTrip(t *testing.T) {
	event, err := NewTurnFailed("user_cancelled", "turn was cancelled", true)
	if err != nil {
		t.Fatalf("new turn failed: %v", err)
	}
	payload, err := DecodeTurnFailed(event)
	if err != nil {
		t.Fatalf("decode turn failed: %v", err)
	}
	if payload.Code != "user_cancelled" || !payload.Cancelled {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}
