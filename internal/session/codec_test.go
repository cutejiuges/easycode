package session

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
)

const (
	testSessionID = domain.SessionID("00000000-0000-7000-8000-000000000001")
	testThreadID  = domain.ThreadID("00000000-0001-7000-8000-000000000002")
	testTurnID    = domain.TurnID("00000000-0002-7000-8000-000000000003")
)

func TestRecordCanonicalGolden(t *testing.T) {
	t.Parallel()
	record, err := BuildRecord(
		Identity{SessionID: testSessionID, ThreadID: testThreadID},
		mustDraft(t, EventTurnFailed, testTurnID, TurnFailedPayload{
			Code: "user_cancelled", Message: "turn was cancelled", Cancelled: true,
		}),
		3,
		time.Date(2026, time.September, 28, 12, 34, 56, 123_000_000, time.UTC),
		3,
		0,
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	sealed, encoded, err := EncodeRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"schema_version":1,"payload_version":1,"seq":3,"timestamp":"2026-09-28T12:34:56.123Z","session_id":"00000000-0000-7000-8000-000000000001","thread_id":"00000000-0001-7000-8000-000000000002","turn_id":"00000000-0002-7000-8000-000000000003","event_kind":"turn_failed","batch_id":3,"batch_index":0,"batch_size":1,"payload":{"code":"user_cancelled","message":"turn was cancelled","cancelled":true},"checksum":"e059e910dcaa06d1d85c60e619bf09271c31a215f831ecb2e18775835efa9da8"}`
	if string(encoded) != want {
		t.Fatalf("canonical record changed:\n%s", encoded)
	}
	decoded, err := DecodeRecord(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Checksum != sealed.Checksum || !bytes.Equal(decoded.Payload, sealed.Payload) {
		t.Fatalf("round trip mismatch: %#v", decoded)
	}
}

func TestRecordRejectsChecksumTampering(t *testing.T) {
	t.Parallel()
	encoded := mustRecord(t, EventTurnFailed, TurnFailedPayload{Code: "failed", Message: "safe"})
	tampered := bytes.Replace(encoded, []byte(`"safe"`), []byte(`"evil"`), 1)
	if _, err := DecodeRecord(tampered); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("DecodeRecord() error = %v", err)
	}
}

func TestCurrentSchemaRejectsCanaryKindAndWrongPayload(t *testing.T) {
	t.Parallel()
	record := mustDecodedRecord(t, EventTurnStarted, TurnStartedPayload{})
	record.PayloadVersion++
	if _, _, err := EncodeRecord(record); err == nil || !strings.Contains(err.Error(), "canary") {
		t.Fatalf("EncodeRecord() error = %v", err)
	}
	record = mustDecodedRecord(t, EventTurnStarted, TurnStartedPayload{})
	record.EventKind = "future_event"
	if _, _, err := EncodeRecord(record); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("EncodeRecord() unknown kind error = %v", err)
	}
	mismatched := mustDraft(t, EventTurnFailed, testTurnID, TurnFailedPayload{Code: "failed"})
	mismatched.descriptor, _ = descriptorByKind(EventTurnStarted)
	if _, err := BuildRecord(
		Identity{SessionID: testSessionID, ThreadID: testThreadID},
		mismatched,
		1, time.Unix(0, 0).UTC(), 1, 0, 1,
	); err == nil {
		t.Fatal("BuildRecord() with a mismatched payload unexpectedly succeeded")
	}
	for _, kind := range []EventKind{
		EventSessionMeta, EventThreadMeta, EventTurnStarted,
		EventProviderNativeCommit, EventSampleUsage, EventToolCallReady,
		EventToolExecutionStarted, EventToolCallResult, EventTurnCompleted, EventTurnFailed,
	} {
		descriptor, ok := descriptorByKind(kind)
		if !ok || descriptor.Kind != kind {
			t.Fatalf("descriptor %q = %#v, %v", kind, descriptor, ok)
		}
	}
}

func TestUnknownRecordShapeIsRejected(t *testing.T) {
	t.Parallel()
	current := mustRecord(t, EventTurnFailed, TurnFailedPayload{Code: "failed"})
	legacy := bytes.Replace(current, []byte(`"payload_version":1`), []byte(`"payload_version":99`), 1)
	if _, err := DecodeRecord(legacy); err == nil {
		t.Fatal("unknown payload canary unexpectedly decoded")
	}
	withOptional := bytes.Replace(current, []byte(`"payload_version":1,`), []byte(`"payload_version":1,"replay_requirement":"optional",`), 1)
	if _, err := DecodeRecord(withOptional); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("legacy optional envelope error = %v", err)
	}
}

func TestRecordSizeLimits(t *testing.T) {
	t.Parallel()
	oversized := Record{
		SchemaVersion: EnvelopeVersion, PayloadVersion: 1,
		Sequence:  1,
		Timestamp: time.Unix(0, 0).UTC(), SessionID: testSessionID,
		ThreadID: testThreadID, EventKind: EventTurnFailed, BatchID: 1,
		BatchIndex: 0, BatchSize: 1,
		Payload: append(append(json.RawMessage(`"`), bytes.Repeat([]byte{'x'}, MaxRecordBytes)...), '"'),
	}
	if _, _, err := EncodeRecord(oversized); err == nil || !strings.Contains(err.Error(), "payload") {
		t.Fatalf("EncodeRecord() error = %v", err)
	}
	if _, err := DecodeRecord(bytes.Repeat([]byte{'x'}, MaxRecordBytes+1)); err == nil {
		t.Fatal("DecodeRecord() above the line limit unexpectedly succeeded")
	}
}

func mustRecord(t *testing.T, kind EventKind, payload any) []byte {
	t.Helper()
	record, err := BuildRecord(
		Identity{SessionID: testSessionID, ThreadID: testThreadID},
		mustDraft(t, kind, testTurnID, payload),
		1, time.Unix(0, 0).UTC(), 1, 0, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, encoded, err := EncodeRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func mustDecodedRecord(t *testing.T, kind EventKind, payload any) Record {
	t.Helper()
	encoded := mustRecord(t, kind, payload)
	record, err := DecodeRecord(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return record
}
