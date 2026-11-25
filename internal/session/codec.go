package session

import (
	"bytes"
	"crypto/sha256"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

var errRecordSizeLimit = errors.New("session record exceeds size limit")

// IsRecordSizeLimit 区分记录编码的大小超限和结构错误。
func IsRecordSizeLimit(err error) bool { return errors.Is(err, errRecordSizeLimit) }

type checksumEnvelope struct {
	Sequence       uint64             `json:"seq"`
	Timestamp      time.Time          `json:"timestamp"`
	SessionID      domain.SessionID   `json:"session_id"`
	ThreadID       domain.ThreadID    `json:"thread_id"`
	ParentThreadID domain.ThreadID    `json:"parent_thread_id,omitempty"`
	TurnID         domain.TurnID      `json:"turn_id,omitempty"`
	EventKind      EventKind          `json:"event_kind"`
	BatchID        uint64             `json:"batch_id"`
	BatchIndex     uint32             `json:"batch_index"`
	BatchSize      uint32             `json:"batch_size"`
	Payload        stdjson.RawMessage `json:"payload"`
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
	if err := draft.validate(); err != nil {
		return Record{}, err
	}
	descriptor := draft.descriptor
	return Record{
		Sequence:       sequence,
		Timestamp:      timestamp.UTC(),
		SessionID:      identity.SessionID,
		ThreadID:       identity.ThreadID,
		ParentThreadID: draft.parentThreadID,
		TurnID:         draft.turnID,
		EventKind:      descriptor.Kind,
		BatchID:        batchID,
		BatchIndex:     batchIndex,
		BatchSize:      batchSize,
		Payload:        draft.PayloadBytes(),
	}, nil
}

// EncodeRecord 校验、计算 checksum 并输出无换行的 canonical JSON。
func EncodeRecord(record Record) (Record, []byte, error) {
	record.Checksum = ""
	if err := validateRecord(record, false); err != nil {
		return Record{}, nil, err
	}
	encoded, err := encodeChecksumEnvelope(record)
	if err != nil {
		return Record{}, nil, err
	}
	sum := sha256.Sum256(encoded)
	record.Checksum = fmt.Sprintf("%x", sum[:])
	// 当前信封的最后一个字段固定为 checksum；散列完成后复用已编码的 payload。
	encoded = append(encoded[:len(encoded)-1], `,"checksum":"`...)
	encoded = append(encoded, record.Checksum...)
	encoded = append(encoded, '"', '}')
	if len(encoded) > MaxRecordBytes {
		return Record{}, nil, errRecordSizeLimit
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
		"seq", "timestamp",
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
	// 解码后的零值无法区分省略的身份和显式空字符串。
	if record.EventKind == EventSessionTitleUpdated && (members["turn_id"] != nil || members["parent_thread_id"] != nil) {
		return Record{}, fmt.Errorf("session title record identity fields must be absent")
	}
	switch record.EventKind {
	case EventPromptSubmitHookPlanned, EventPromptSubmitHookDecided, EventPromptSubmitHookClosed:
		if members["parent_thread_id"] != nil {
			return Record{}, fmt.Errorf("prompt submit record has an unexpected parent thread identity")
		}
	case EventThreadForked:
		if members["turn_id"] != nil {
			return Record{}, fmt.Errorf("thread fork record turn identity must be absent")
		}
	case EventCompactionCompleted, EventPostCompactHookStarted, EventPostCompactHookFinished, EventPostCompactHookClosed:
		if members["turn_id"] != nil || members["parent_thread_id"] != nil {
			return Record{}, fmt.Errorf("compaction record identity fields must be absent")
		}
	case EventSessionStartHookPlanned, EventSessionStartHookStarted, EventSessionStartHookFinished, EventSessionStartHookClosed:
		if (record.TurnID == "" && members["turn_id"] != nil) || members["parent_thread_id"] != nil {
			return Record{}, fmt.Errorf("session start record has unexpected identity fields")
		}
		// validateRecord 已按真实分支校验这四种记录的 payload 和批次形状。
		return cloneRecord(record), nil
	}
	if err := decodeKnownRecord(record); err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

func scanEnvelope(line []byte) (map[string]stdjson.RawMessage, error) {
	members, err := codec.ObjectMembers(line)
	if err != nil {
		return nil, err
	}
	for name, raw := range members {
		if !knownEnvelopeField(name) {
			return nil, fmt.Errorf("session envelope contains an unknown field")
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, fmt.Errorf("session envelope field must not be null")
		}
	}
	return members, nil
}

func knownEnvelopeField(name string) bool {
	switch name {
	case "seq", "timestamp",
		"session_id", "thread_id", "parent_thread_id", "turn_id", "event_kind",
		"batch_id", "batch_index", "batch_size", "payload", "checksum":
		return true
	default:
		return false
	}
}

func validateRecord(record Record, requireChecksum bool) error {
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
	descriptor, exists := descriptorByKind(record.EventKind)
	if !exists {
		return fmt.Errorf("session event kind is unsupported")
	}
	if descriptor.Placement == PlacementSessionStartHook {
		if err := validateSessionStartRecordShape(record); err != nil {
			return err
		}
	}
	if record.EventKind == EventSessionTitleUpdated {
		if err := validateSessionTitleEnvelope(record); err != nil {
			return err
		}
	}
	if record.EventKind == EventThreadForked {
		if err := validateThreadForkedEnvelope(record); err != nil {
			return err
		}
	}
	// 完整压缩开始、检查和成功记录不能被误判为可修复尾批。
	if descriptor.Placement == PlacementCompactionStart || descriptor.Placement == PlacementCompactionHook || record.EventKind == EventCompactionCompleted {
		if record.BatchID != record.Sequence || record.BatchIndex != 0 || record.BatchSize != 1 || record.TurnID != "" || record.ParentThreadID != "" {
			return fmt.Errorf("invalid compaction admission record envelope")
		}
	}
	switch record.EventKind {
	case EventPromptSubmitHookPlanned, EventPromptSubmitHookDecided, EventPromptSubmitHookClosed:
		if err := validatePromptSubmitEnvelope(record); err != nil {
			return err
		}
	case EventHookExecutionStarted, EventHookExecutionFinished:
		// 共享种类只有提示词检查限定单记录。解码路径已执行同一 typed 校验。
		if !requireChecksum && (record.BatchSize != 1 || record.ParentThreadID != "") {
			if err := decodeKnownRecord(record); err != nil {
				return err
			}
		}
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
	encoded, err := encodeChecksumEnvelope(record)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:]), nil
}

func encodeChecksumEnvelope(record Record) ([]byte, error) {
	encoded, err := codec.MarshalStable(checksumEnvelope{
		Sequence:  record.Sequence,
		Timestamp: record.Timestamp, SessionID: record.SessionID, ThreadID: record.ThreadID,
		ParentThreadID: record.ParentThreadID, TurnID: record.TurnID, EventKind: record.EventKind,
		BatchID: record.BatchID, BatchIndex: record.BatchIndex, BatchSize: record.BatchSize,
		Payload: record.Payload,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal session checksum input: %w", err)
	}
	return encoded, nil
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
