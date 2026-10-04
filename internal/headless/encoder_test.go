package headless

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/fault"
)

const (
	testSessionID = domain.SessionID("00000000-0010-7000-8000-000000000010")
	testThreadID  = domain.ThreadID("00000000-0011-7000-8000-000000000011")
	testTurnID    = domain.TurnID("00000000-0012-7000-8000-000000000012")
)

func TestEncoderMatchesJSONLGolden(t *testing.T) {
	events := []Event{
		mustThreadStarted(t, true),
		mustTurnStarted(t),
		mustTextDelta(t, "hello"),
		mustTurnCompleted(t),
		mustTurnFailed(t, fault.Summary{Code: fault.CodeProviderRequest, Message: "provider request failed"}),
		mustErrorEvent(t, fault.Summary{Code: fault.CodeStreamProtocol, Message: "runtime event stream is invalid"}),
	}
	var output bytes.Buffer
	encoder := NewEncoder(&output)
	for _, event := range events {
		if err := encoder.Write(event); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/events.golden.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("JSONL differs\ngot:  %s\nwant: %s", output.Bytes(), want)
	}
}

func TestEncoderKeepsUnicodeAndControlCharactersOnOneLine(t *testing.T) {
	text := "line\n\u2028\u2029\"\\\t"
	var output bytes.Buffer
	if err := NewEncoder(&output).Write(mustTextDelta(t, text)); err != nil {
		t.Fatal(err)
	}
	if bytes.Count(output.Bytes(), []byte{'\n'}) != 1 ||
		!bytes.Contains(output.Bytes(), []byte(`\n`)) ||
		!bytes.Contains(output.Bytes(), []byte(`\u2028`)) ||
		!bytes.Contains(output.Bytes(), []byte(`\u2029`)) {
		t.Fatalf("event is not single-line escaped JSON: %q", output.String())
	}
	var decoded AssistantTextDeltaEvent
	if err := codec.Unmarshal(bytes.TrimSpace(output.Bytes()), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Text != text {
		t.Fatalf("decoded text = %q, want %q", decoded.Text, text)
	}
}

func TestEncoderHandlesShortWritesAndStopsAfterFailure(t *testing.T) {
	chunked := &chunkWriter{limit: 3}
	if err := NewEncoder(chunked).Write(mustTurnStarted(t)); err != nil {
		t.Fatalf("chunked write: %v", err)
	}
	if !strings.HasSuffix(chunked.String(), "\n") {
		t.Fatalf("chunked output = %q", chunked.String())
	}

	broken := &brokenWriter{remaining: 9}
	encoder := NewEncoder(broken)
	if err := encoder.Write(mustTurnStarted(t)); err == nil {
		t.Fatal("expected broken writer error")
	}
	written := broken.buffer.Len()
	if err := encoder.Write(mustTurnCompleted(t)); err == nil {
		t.Fatal("expected unavailable output error")
	}
	if broken.buffer.Len() != written {
		t.Fatal("encoder wrote another event after output failure")
	}
}

func TestEncoderRejectsMutatedTypedEventBeforeWriting(t *testing.T) {
	event, err := NewTurnCompletedEvent(testSessionID, testThreadID, testTurnID, testHeadlessUsage(t))
	if err != nil {
		t.Fatal(err)
	}
	event.Type = "turn.failed"
	var output bytes.Buffer
	if err := NewEncoder(&output).Write(event); err == nil {
		t.Fatal("expected mutated event error")
	}
	if output.Len() != 0 {
		t.Fatalf("invalid event was written: %q", output.String())
	}
}

func TestTurnCompletedV1ContractRequiresUsageAndAllowsUnknownFields(t *testing.T) {
	missingUsage := []byte(`{"version":1,"type":"turn.completed","session_id":"00000000-0010-7000-8000-000000000010","thread_id":"00000000-0011-7000-8000-000000000011","turn_id":"00000000-0012-7000-8000-000000000012"}`)
	if _, err := decodeV1EventForContractTest(missingUsage); err == nil {
		t.Fatal("completion without usage unexpectedly matched v1 contract")
	}

	valid, err := codec.MarshalStable(mustTurnCompleted(t))
	if err != nil {
		t.Fatal(err)
	}
	withUnknown := append(valid[:len(valid)-1], []byte(`,"future_field":true}`)...)
	event, err := decodeV1EventForContractTest(withUnknown)
	if err != nil {
		t.Fatalf("known event rejected unknown field: %v", err)
	}
	completed, ok := event.(TurnCompletedEvent)
	if !ok || completed.Usage.InputUncached.Value == nil || *completed.Usage.InputUncached.Value != 0 {
		t.Fatalf("decoded completion = %#v", event)
	}

	unknownType := []byte(`{"version":1,"type":"turn.unknown"}`)
	if _, err := decodeV1EventForContractTest(unknownType); err == nil {
		t.Fatal("unknown event type unexpectedly matched v1 contract")
	}
}

func decodeV1EventForContractTest(content []byte) (Event, error) {
	var envelope struct {
		Version int    `json:"version"`
		Type    string `json:"type"`
	}
	if err := codec.Unmarshal(content, &envelope); err != nil {
		return nil, err
	}
	if envelope.Version != eventVersion {
		return nil, errors.New("event version is invalid")
	}
	var event Event
	switch envelope.Type {
	case "turn.completed":
		var completed TurnCompletedEvent
		if err := codec.Unmarshal(content, &completed); err != nil {
			return nil, err
		}
		event = completed
	default:
		return nil, errors.New("event type is unsupported")
	}
	if err := validateEvent(event); err != nil {
		return nil, err
	}
	return event, nil
}

type chunkWriter struct {
	bytes.Buffer
	limit int
}

func (writer *chunkWriter) Write(content []byte) (int, error) {
	if len(content) > writer.limit {
		content = content[:writer.limit]
	}
	return writer.Buffer.Write(content)
}

type brokenWriter struct {
	buffer    bytes.Buffer
	remaining int
}

func (writer *brokenWriter) Write(content []byte) (int, error) {
	if writer.remaining <= 0 {
		return 0, io.ErrClosedPipe
	}
	if len(content) > writer.remaining {
		content = content[:writer.remaining]
	}
	written, _ := writer.buffer.Write(content)
	writer.remaining -= written
	return written, errors.New("fixture output secret")
}

func mustThreadStarted(t *testing.T, resumed bool) Event {
	t.Helper()
	event, err := NewThreadStartedEvent(testSessionID, testThreadID, resumed)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func mustTurnStarted(t *testing.T) Event {
	t.Helper()
	event, err := NewTurnStartedEvent(testSessionID, testThreadID, testTurnID)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func mustTextDelta(t *testing.T, text string) Event {
	t.Helper()
	event, err := NewAssistantTextDeltaEvent(testSessionID, testThreadID, testTurnID, text)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func mustTurnCompleted(t *testing.T) Event {
	t.Helper()
	event, err := NewTurnCompletedEvent(testSessionID, testThreadID, testTurnID, testHeadlessUsage(t))
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func testHeadlessUsage(t *testing.T) domain.SampleUsage {
	t.Helper()
	usage, err := domain.NewSampleUsage(
		domain.KnownUsageMetric(0),
		domain.UnknownUsageMetric(),
		domain.NotApplicableUsageMetric(),
		domain.KnownUsageMetric(5),
		domain.KnownUsageMetric(2),
	)
	if err != nil {
		t.Fatal(err)
	}
	return usage
}

func mustTurnFailed(t *testing.T, failure fault.Summary) Event {
	t.Helper()
	event, err := NewTurnFailedEvent(testSessionID, testThreadID, testTurnID, failure)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func mustErrorEvent(t *testing.T, failure fault.Summary) Event {
	t.Helper()
	event, err := NewErrorEvent(failure)
	if err != nil {
		t.Fatal(err)
	}
	return event
}
