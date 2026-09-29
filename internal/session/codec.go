package session

import (
	"bytes"
	"crypto/sha256"
	stdjson "encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"time"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

type checksumEnvelope struct {
	SchemaVersion     int                `json:"schema_version"`
	PayloadVersion    int                `json:"payload_version"`
	ReplayRequirement ReplayRequirement  `json:"replay_requirement"`
	Sequence          uint64             `json:"seq"`
	Timestamp         time.Time          `json:"timestamp"`
	SessionID         domain.SessionID   `json:"session_id"`
	ThreadID          domain.ThreadID    `json:"thread_id"`
	ParentThreadID    domain.ThreadID    `json:"parent_thread_id,omitempty"`
	TurnID            domain.TurnID      `json:"turn_id,omitempty"`
	EventKind         EventKind          `json:"event_kind"`
	BatchID           uint64             `json:"batch_id"`
	BatchIndex        uint32             `json:"batch_index"`
	BatchSize         uint32             `json:"batch_size"`
	Payload           stdjson.RawMessage `json:"payload"`
}

// BuildRecord 根据 registry 编码 draft，并填充尚未签名的完整信封。
func BuildRecord(
	identity Identity,
	draft RecordDraft,
	sequence uint64,
	timestamp time.Time,
	batchID uint64,
	batchIndex uint32,
	batchSize uint32,
) (Record, error) {
	descriptor, err := descriptorForDraft(draft)
	if err != nil {
		return Record{}, err
	}
	payload, err := codec.MarshalStable(draft.Payload)
	if err != nil {
		return Record{}, fmt.Errorf("marshal session payload: %w", err)
	}
	return Record{
		SchemaVersion:     EnvelopeVersion,
		PayloadVersion:    descriptor.Version,
		ReplayRequirement: descriptor.Requirement,
		Sequence:          sequence,
		Timestamp:         timestamp.UTC(),
		SessionID:         identity.SessionID,
		ThreadID:          identity.ThreadID,
		ParentThreadID:    draft.ParentThreadID,
		TurnID:            draft.TurnID,
		EventKind:         draft.EventKind,
		BatchID:           batchID,
		BatchIndex:        batchIndex,
		BatchSize:         batchSize,
		Payload:           payload,
	}, nil
}

// EncodeRecord 校验、计算 checksum 并输出无换行的 canonical JSON。
func EncodeRecord(record Record) (Record, []byte, error) {
	record.Checksum = ""
	if err := validateRecord(record, false); err != nil {
		return Record{}, nil, err
	}
	checksum, err := recordChecksum(record)
	if err != nil {
		return Record{}, nil, err
	}
	record.Checksum = checksum
	encoded, err := codec.MarshalStable(record)
	if err != nil {
		return Record{}, nil, fmt.Errorf("marshal session record: %w", err)
	}
	if len(encoded) > MaxRecordBytes {
		return Record{}, nil, fmt.Errorf("session record exceeds size limit")
	}
	return record, encoded, nil
}

// DecodeRecord 严格解码并校验一行完整的 JSONL 记录。
func DecodeRecord(line []byte) (Record, error) {
	if len(line) == 0 || len(line) > MaxRecordBytes {
		return Record{}, fmt.Errorf("session record size is invalid")
	}
	if !utf8.Valid(line) {
		return Record{}, fmt.Errorf("session record is not valid UTF-8")
	}
	members, err := scanEnvelope(line)
	if err != nil {
		return Record{}, err
	}
	requiredFields := [...]string{
		"schema_version", "payload_version", "replay_requirement", "seq", "timestamp",
		"session_id", "thread_id", "event_kind", "batch_id", "batch_index",
		"batch_size", "payload", "checksum",
	}
	for _, field := range requiredFields {
		if _, exists := members[field]; !exists {
			return Record{}, fmt.Errorf("session record is missing required field")
		}
	}
	var record Record
	if err := codec.Unmarshal(line, &record); err != nil {
		return Record{}, fmt.Errorf("decode session record: %w", err)
	}
	if err := validateCanonicalTimestamp(members["timestamp"], record.Timestamp); err != nil {
		return Record{}, err
	}
	if err := validateRecord(record, true); err != nil {
		return Record{}, err
	}
	want, err := recordChecksum(record)
	if err != nil {
		return Record{}, err
	}
	if record.Checksum != want {
		return Record{}, fmt.Errorf("session record checksum mismatch")
	}
	return cloneRecord(record), nil
}

// DecodePayload 将已知记录解码到其 registry 声明的强类型 payload。
func DecodePayload(record Record) (any, error) {
	descriptor, exact := LookupDescriptor(record.EventKind, record.PayloadVersion)
	if !exact {
		return nil, fmt.Errorf("session payload revision is unsupported")
	}
	target := reflect.New(descriptor.payloadType).Interface()
	decoder := stdjson.NewDecoder(bytes.NewReader(record.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return nil, fmt.Errorf("decode session payload: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("session payload contains trailing JSON")
	}
	return reflect.ValueOf(target).Elem().Interface(), nil
}

func scanEnvelope(line []byte) (map[string]stdjson.RawMessage, error) {
	decoder := stdjson.NewDecoder(bytes.NewReader(line))
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("decode session envelope: %w", err)
	}
	if delimiter, ok := token.(stdjson.Delim); !ok || delimiter != '{' {
		return nil, fmt.Errorf("session envelope must be an object")
	}
	members := make(map[string]stdjson.RawMessage, 15)
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("decode session envelope member: %w", err)
		}
		name, ok := token.(string)
		if !ok || !knownEnvelopeField(name) {
			return nil, fmt.Errorf("session envelope contains an unknown field")
		}
		if _, duplicate := members[name]; duplicate {
			return nil, fmt.Errorf("session envelope contains a duplicate field")
		}
		var raw stdjson.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, fmt.Errorf("decode session envelope value: %w", err)
		}
		members[name] = append(stdjson.RawMessage(nil), raw...)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("decode session envelope end: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("session envelope contains trailing JSON")
	}
	return members, nil
}

func knownEnvelopeField(name string) bool {
	switch name {
	case "schema_version", "payload_version", "replay_requirement", "seq", "timestamp",
		"session_id", "thread_id", "parent_thread_id", "turn_id", "event_kind",
		"batch_id", "batch_index", "batch_size", "payload", "checksum":
		return true
	default:
		return false
	}
}

func validateRecord(record Record, requireChecksum bool) error {
	if record.SchemaVersion != EnvelopeVersion {
		return fmt.Errorf("session envelope version is unsupported")
	}
	if record.PayloadVersion <= 0 {
		return fmt.Errorf("session payload version is invalid")
	}
	if record.ReplayRequirement != ReplayRequired && record.ReplayRequirement != ReplayOptional {
		return fmt.Errorf("session replay requirement is invalid")
	}
	if record.Sequence == 0 || record.BatchID == 0 || record.BatchSize == 0 || record.BatchIndex >= record.BatchSize {
		return fmt.Errorf("session sequence or batch boundary is invalid")
	}
	if record.Timestamp.IsZero() || record.Timestamp.Location() != time.UTC {
		return fmt.Errorf("session timestamp must be UTC")
	}
	if !record.SessionID.Valid() || !record.ThreadID.Valid() {
		return fmt.Errorf("session record identity is invalid")
	}
	if record.ParentThreadID != "" && !record.ParentThreadID.Valid() {
		return fmt.Errorf("session parent thread ID is invalid")
	}
	if record.TurnID != "" && !record.TurnID.Valid() {
		return fmt.Errorf("session turn ID is invalid")
	}
	if record.EventKind == "" || !codec.Valid(record.Payload) || len(record.Payload) == 0 || len(record.Payload) > MaxRecordBytes {
		return fmt.Errorf("session event kind or payload is invalid")
	}
	if err := validateKnownDeclaration(record); err != nil {
		return err
	}
	if requireChecksum {
		if len(record.Checksum) != sha256.Size*2 {
			return fmt.Errorf("session record checksum is invalid")
		}
		for _, character := range record.Checksum {
			if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
				return fmt.Errorf("session record checksum is invalid")
			}
		}
	}
	return nil
}

func recordChecksum(record Record) (string, error) {
	encoded, err := codec.MarshalStable(checksumEnvelope{
		SchemaVersion: record.SchemaVersion, PayloadVersion: record.PayloadVersion,
		ReplayRequirement: record.ReplayRequirement, Sequence: record.Sequence,
		Timestamp: record.Timestamp, SessionID: record.SessionID, ThreadID: record.ThreadID,
		ParentThreadID: record.ParentThreadID, TurnID: record.TurnID, EventKind: record.EventKind,
		BatchID: record.BatchID, BatchIndex: record.BatchIndex, BatchSize: record.BatchSize,
		Payload: record.Payload,
	})
	if err != nil {
		return "", fmt.Errorf("marshal session checksum input: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:]), nil
}

func validateCanonicalTimestamp(raw stdjson.RawMessage, timestamp time.Time) error {
	want := strconv.Quote(timestamp.UTC().Format(time.RFC3339Nano))
	if string(raw) != want || timestamp.Location() != time.UTC {
		return fmt.Errorf("session timestamp is not canonical UTC")
	}
	return nil
}

func cloneRecord(record Record) Record {
	record.Payload = append(stdjson.RawMessage(nil), record.Payload...)
	return record
}
