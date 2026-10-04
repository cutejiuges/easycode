package protocol

import (
	"encoding/json"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
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

func TestPayloadlessTurnStartedIsTypedAndStrict(t *testing.T) {
	started := NewTurnStarted()
	if err := ValidateTurnStarted(started); err != nil {
		t.Fatal(err)
	}
	started.Payload = []byte(`{}`)
	if err := ValidateTurnStarted(started); err == nil {
		t.Fatal("expected turn started payload error")
	}
}

func TestTurnCompletedUsageRoundTripPreservesMetricStates(t *testing.T) {
	usage := protocolTestUsage(t)
	completed, err := NewTurnCompleted(usage)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := DecodeTurnCompleted(completed)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := payload.Usage.Domain()
	if err != nil || decoded != usage {
		t.Fatalf("decoded usage = %#v, %v", decoded, err)
	}
	if payload.Usage.InputUncached.Value == nil || *payload.Usage.InputUncached.Value != 0 ||
		payload.Usage.CacheRead.Value != nil || payload.Usage.CacheWrite.Value != nil {
		t.Fatalf("metric state/value encoding = %#v", payload.Usage)
	}
	var wire map[string]json.RawMessage
	if err := codec.Unmarshal(completed.Payload, &wire); err != nil {
		t.Fatal(err)
	}
	if _, ok := wire["usage"]; !ok {
		t.Fatalf("completion payload = %s", completed.Payload)
	}
}

func TestTurnCompletedRejectsMissingInvalidAndUnknownUsage(t *testing.T) {
	if _, err := NewTurnCompleted(domain.SampleUsage{}); err == nil {
		t.Fatal("expected zero usage error")
	}
	fixtures := []string{
		`{}`,
		`{"usage":{"input_uncached":{"state":"known"},"cache_read":{"state":"unknown"},"cache_write":{"state":"not_applicable"},"output":{"state":"known","value":1},"reasoning_output":{"state":"unknown"}}}`,
		`{"usage":{"input_uncached":{"state":"known","value":0},"cache_read":{"state":"unknown","value":0},"cache_write":{"state":"not_applicable"},"output":{"state":"known","value":1},"reasoning_output":{"state":"unknown"}}}`,
		`{"usage":{"input_uncached":{"state":"known","value":0},"cache_read":{"state":"unknown"},"cache_write":{"state":"not_applicable"},"output":{"state":"known","value":1},"reasoning_output":{"state":"unknown"}},"extra":true}`,
	}
	for _, fixture := range fixtures {
		event := newEvent(EventTurnCompleted)
		event.Payload = []byte(fixture)
		if _, err := DecodeTurnCompleted(event); err == nil {
			t.Fatalf("DecodeTurnCompleted(%s) unexpectedly succeeded", fixture)
		}
	}
	event, err := NewTurnCompleted(protocolTestUsage(t))
	if err != nil {
		t.Fatal(err)
	}
	event.Kind = EventTurnFailed
	if _, err := DecodeTurnCompleted(event); err == nil {
		t.Fatal("expected turn completed kind error")
	}
}

func protocolTestUsage(t *testing.T) domain.SampleUsage {
	t.Helper()
	usage, err := domain.NewSampleUsage(
		domain.KnownUsageMetric(0),
		domain.UnknownUsageMetric(),
		domain.NotApplicableUsageMetric(),
		domain.KnownUsageMetric(4),
		domain.UnknownUsageMetric(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return usage
}
