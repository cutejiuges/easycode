package session

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"easycode/internal/codec"
)

func TestRecordEncodingMatchesSonic(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		payload string
		options uint8
	}{
		{"minimal", `{}`, 0},
		{"optional_fields", ` { "items": [true, false, null, {"count": 1e20}] } `, 3},
		{"escaped_unicode", `{"text":"中文😀<>&\u2028\u2029\"\\\n\ud83d\ude00"}`, 1},
		{"array", `[[], {}, 1.20, -0, 12345678901234567890]`, 2},
		{"scalar", `"payload"`, 3},
		{"null", `null`, 0},
		{"invalid_json", `{"broken":`, 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertRecordEncodingMatchesSonic(t, recordEncodingInput([]byte(test.payload), test.options))
		})
	}
	invalidTime := recordEncodingInput([]byte(`{}`), 0)
	invalidTime.Timestamp = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	assertRecordEncodingMatchesSonic(t, invalidTime)
}

func TestRecordEncodedSizeIncludesChecksum(t *testing.T) {
	t.Parallel()
	record := recordEncodingInput([]byte(`""`), 0)
	_, empty, err := EncodeRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	record.Payload = []byte(`"` + strings.Repeat("x", MaxRecordBytes-len(empty)) + `"`)
	_, encoded, err := EncodeRecord(record)
	if err != nil || len(encoded) != MaxRecordBytes {
		t.Fatalf("record at limit: bytes=%d error=%v", len(encoded), err)
	}
	record.Payload = append(record.Payload[:len(record.Payload)-1], 'x', '"')
	if _, _, err := EncodeRecord(record); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("record above limit: %v", err)
	}
}

func FuzzRecordEncodingMatchesSonic(f *testing.F) {
	f.Add([]byte(`{}`), uint64(1), uint8(0))
	f.Add([]byte(` { "items": [null, 1.0, "中文<>&😀"] } `), ^uint64(0), uint8(3))
	f.Add([]byte(`{"escaped":"\u2028\ud83d\ude00\\\""}`), uint64(2), uint8(1))
	f.Add([]byte(`null`), uint64(3), uint8(2))
	f.Add([]byte(`{"broken":`), uint64(0), uint8(0))
	f.Fuzz(func(t *testing.T, payload []byte, sequence uint64, options uint8) {
		if len(payload) > 4096 {
			return
		}
		record := recordEncodingInput(payload, options)
		record.Sequence = sequence
		assertRecordEncodingMatchesSonic(t, record)
	})
}

func recordEncodingInput(payload []byte, options uint8) Record {
	record := Record{
		Sequence: 1, Timestamp: time.Date(2026, 11, 17, 0, 0, 0, int(options), time.UTC),
		SessionID: testSessionID, ThreadID: testThreadID, EventKind: EventTurnFailed,
		BatchID: ^uint64(0), BatchIndex: 1, BatchSize: 2, Payload: payload,
		Checksum: "replaced-by-encoder",
	}
	if options&1 != 0 {
		record.ParentThreadID = testThreadID
	}
	if options&2 != 0 {
		record.TurnID = testTurnID
	}
	return record
}

func assertRecordEncodingMatchesSonic(t *testing.T, record Record) {
	t.Helper()
	before := cloneRecord(record)
	wantRecord, want, wantErr := encodeRecordWithSonic(record)
	gotRecord, got, gotErr := EncodeRecord(record)
	if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
		t.Fatalf("encoding error changed: got=%v want=%v", gotErr, wantErr)
	}
	if !bytes.Equal(got, want) || !reflect.DeepEqual(gotRecord, wantRecord) {
		t.Fatalf("encoding changed: got=%s want=%s", got, want)
	}
	if !bytes.Equal(record.Payload, before.Payload) || record.Checksum != before.Checksum {
		t.Fatal("encoding modified input")
	}
	if len(got) != 0 && len(record.Payload) != 0 {
		// 返回的 JSON 字节不能引用调用者仍持有的 payload。
		record.Payload[0] ^= 1
		if !bytes.Equal(got, want) {
			t.Fatal("encoded bytes alias input payload")
		}
		record.Payload[0] ^= 1
	}
}

// encodeRecordWithSonic 保留重构前的两次整体编码，作为独立的字节与错误参考。
func encodeRecordWithSonic(record Record) (Record, []byte, error) {
	record.Checksum = ""
	if err := validateRecord(record, false); err != nil {
		return Record{}, nil, err
	}
	envelope, err := codec.MarshalStable(checksumEnvelope{
		Sequence: record.Sequence, Timestamp: record.Timestamp,
		SessionID: record.SessionID, ThreadID: record.ThreadID,
		ParentThreadID: record.ParentThreadID, TurnID: record.TurnID, EventKind: record.EventKind,
		BatchID: record.BatchID, BatchIndex: record.BatchIndex, BatchSize: record.BatchSize,
		Payload: record.Payload,
	})
	if err != nil {
		return Record{}, nil, fmt.Errorf("marshal session checksum input: %w", err)
	}
	sum := sha256.Sum256(envelope)
	record.Checksum = fmt.Sprintf("%x", sum[:])
	encoded, err := codec.MarshalStable(record)
	if err != nil {
		return Record{}, nil, fmt.Errorf("marshal session record: %w", err)
	}
	if len(encoded) > MaxRecordBytes {
		return Record{}, nil, fmt.Errorf("session record exceeds size limit")
	}
	return record, encoded, nil
}
