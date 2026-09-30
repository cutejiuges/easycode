package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// DecodeSessionMetaPayload 严格解码并验证 session_meta v1 payload。
func DecodeSessionMetaPayload(record Record) (SessionMetaPayload, error) {
	payload, err := decodeKnownPayload[SessionMetaPayload](record, EventSessionMeta)
	if err != nil {
		return SessionMetaPayload{}, err
	}
	if err := validateSessionMetaPayload(payload); err != nil {
		return SessionMetaPayload{}, err
	}
	return payload, nil
}

// DecodeThreadMetaPayload 严格解码并验证 thread_meta v1 payload。
func DecodeThreadMetaPayload(record Record) (ThreadMetaPayload, error) {
	payload, err := decodeKnownPayload[ThreadMetaPayload](record, EventThreadMeta)
	if err != nil {
		return ThreadMetaPayload{}, err
	}
	if err := validateThreadMetaPayload(payload); err != nil {
		return ThreadMetaPayload{}, err
	}
	return payload, nil
}

// DecodeTurnStartedPayload 严格解码并验证 turn_started v1 payload。
func DecodeTurnStartedPayload(record Record) (TurnStartedPayload, error) {
	return decodeKnownPayload[TurnStartedPayload](record, EventTurnStarted)
}

// DecodeNativeCommitPayload 严格解码并验证 provider_native_commit v1 payload。
func DecodeNativeCommitPayload(record Record) (NativeCommitPayload, error) {
	payload, err := decodeKnownPayload[NativeCommitPayload](record, EventProviderNativeCommit)
	if err != nil {
		return NativeCommitPayload{}, err
	}
	if err := validateNativeCommitPayload(payload); err != nil {
		return NativeCommitPayload{}, err
	}
	payload.Payload = append(json.RawMessage(nil), payload.Payload...)
	return payload, nil
}

// DecodeTurnCompletedPayload 严格解码并验证 turn_completed v1 payload。
func DecodeTurnCompletedPayload(record Record) (TurnCompletedPayload, error) {
	return decodeKnownPayload[TurnCompletedPayload](record, EventTurnCompleted)
}

// DecodeTurnFailedPayload 严格解码并验证 turn_failed v1 payload。
func DecodeTurnFailedPayload(record Record) (TurnFailedPayload, error) {
	payload, err := decodeKnownPayload[TurnFailedPayload](record, EventTurnFailed)
	if err != nil {
		return TurnFailedPayload{}, err
	}
	if err := validateTurnFailedPayload(payload); err != nil {
		return TurnFailedPayload{}, err
	}
	return payload, nil
}

func decodeKnownPayload[T any](record Record, kind EventKind) (T, error) {
	var zero T
	descriptor, exists := descriptorByKind(kind)
	if !exists || record.EventKind != kind || record.PayloadVersion != descriptor.Version ||
		record.ReplayRequirement != descriptor.Requirement {
		return zero, fmt.Errorf("session payload declaration is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(record.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&zero); err != nil {
		return zero, fmt.Errorf("decode session payload: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return zero, fmt.Errorf("session payload contains trailing JSON")
	}
	return zero, nil
}

func validateSessionMetaPayload(payload SessionMetaPayload) error {
	if !payload.RootThreadID.Valid() || payload.CreatedAt.IsZero() || payload.CreatedAt.Location() != time.UTC ||
		!payload.Provider.Valid() || strings.TrimSpace(payload.ProviderWire) == "" || strings.TrimSpace(payload.Model) == "" ||
		payload.SchemaRevision <= 0 {
		return fmt.Errorf("session metadata payload is invalid")
	}
	if !filepath.IsAbs(payload.CreationCWD) || filepath.Clean(payload.CreationCWD) != payload.CreationCWD {
		return fmt.Errorf("session creation cwd is invalid")
	}
	return nil
}

func validateThreadMetaPayload(payload ThreadMetaPayload) error {
	if payload.Root {
		if payload.ParentThreadID != "" {
			return fmt.Errorf("root thread metadata parent must be empty")
		}
		return nil
	}
	if !payload.ParentThreadID.Valid() {
		return fmt.Errorf("child thread metadata parent is invalid")
	}
	return nil
}

func validateNativeCommitPayload(payload NativeCommitPayload) error {
	if !payload.Provider.Valid() || strings.TrimSpace(payload.Wire) == "" || payload.PayloadVersion <= 0 ||
		len(payload.Payload) == 0 || len(payload.Payload) > MaxRecordBytes || !json.Valid(payload.Payload) {
		return fmt.Errorf("provider native commit payload is invalid")
	}
	return nil
}

func validateTurnFailedPayload(payload TurnFailedPayload) error {
	if strings.TrimSpace(payload.Code) == "" {
		return fmt.Errorf("turn failure code is required")
	}
	return nil
}
