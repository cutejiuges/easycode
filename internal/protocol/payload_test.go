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

func TestTurnFailedRejectsWhitespaceSummary(t *testing.T) {
	if _, err := NewTurnFailed(" ", "turn failed", false); err == nil {
		t.Fatal("expected whitespace code error")
	}
	if _, err := NewTurnFailed("turn_failed", "\t", false); err == nil {
		t.Fatal("expected whitespace message error")
	}
}

func TestPayloadDecodersRejectWrongVersionAndUnknownFields(t *testing.T) {
	delta, err := NewAssistantTextDelta("hello")
	if err != nil {
		t.Fatal(err)
	}
	delta.Version++
	if _, err := DecodeAssistantTextDelta(delta); err == nil {
		t.Fatal("expected delta version error")
	}

	failure := newEvent(EventTurnFailed)
	failure.Payload = []byte(`{"code":"failed","message":"failed","unknown":true}`)
	if _, err := DecodeTurnFailed(failure); err == nil {
		t.Fatal("expected failure unknown field error")
	}
}

func TestPayloadlessEventsAreTypedAndStrict(t *testing.T) {
	started := NewTurnStarted()
	if err := ValidateTurnStarted(started); err != nil {
		t.Fatal(err)
	}
	started.Payload = []byte(`{}`)
	if err := ValidateTurnStarted(started); err == nil {
		t.Fatal("expected turn started payload error")
	}

	completed := NewTurnCompleted()
	if err := ValidateTurnCompleted(completed); err != nil {
		t.Fatal(err)
	}
	completed.Kind = EventTurnFailed
	if err := ValidateTurnCompleted(completed); err == nil {
		t.Fatal("expected turn completed kind error")
	}
}
