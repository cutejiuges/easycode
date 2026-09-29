package session

import (
	"encoding/json"
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// NewSessionMetaDraft 创建已验证并编码的 session_meta v1 draft。
func NewSessionMetaDraft(payload SessionMetaPayload) (RecordDraft, error) {
	if err := validateSessionMetaPayload(payload); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventSessionMeta, "", "", payload)
}

// NewThreadMetaDraft 创建已验证并编码的 thread_meta v1 draft。
func NewThreadMetaDraft(payload ThreadMetaPayload) (RecordDraft, error) {
	if err := validateThreadMetaPayload(payload); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventThreadMeta, payload.ParentThreadID, "", payload)
}

// NewTurnStartedDraft 创建已验证并编码的 turn_started v1 draft。
func NewTurnStartedDraft(turnID domain.TurnID) (RecordDraft, error) {
	if err := validateTurnID(turnID); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventTurnStarted, "", turnID, TurnStartedPayload{})
}

// NewProviderNativeCommitDraft 创建已验证并编码的 provider_native_commit v1 draft。
func NewProviderNativeCommitDraft(turnID domain.TurnID, payload NativeCommitPayload) (RecordDraft, error) {
	if err := validateTurnID(turnID); err != nil {
		return RecordDraft{}, err
	}
	if err := validateNativeCommitPayload(payload); err != nil {
		return RecordDraft{}, err
	}
	payload.Payload = append(json.RawMessage(nil), payload.Payload...)
	return encodeDraft(EventProviderNativeCommit, "", turnID, payload)
}

// NewTurnCompletedDraft 创建已验证并编码的 turn_completed v1 draft。
func NewTurnCompletedDraft(turnID domain.TurnID) (RecordDraft, error) {
	if err := validateTurnID(turnID); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventTurnCompleted, "", turnID, TurnCompletedPayload{})
}

// NewTurnFailedDraft 创建已验证并编码的 turn_failed v1 draft。
func NewTurnFailedDraft(turnID domain.TurnID, payload TurnFailedPayload) (RecordDraft, error) {
	if err := validateTurnID(turnID); err != nil {
		return RecordDraft{}, err
	}
	if err := validateTurnFailedPayload(payload); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventTurnFailed, "", turnID, payload)
}

func encodeDraft(
	kind EventKind,
	parentThreadID domain.ThreadID,
	turnID domain.TurnID,
	payload any,
) (RecordDraft, error) {
	descriptor, exists := descriptorByKind(kind)
	if !exists {
		return RecordDraft{}, fmt.Errorf("unsupported session event kind")
	}
	encoded, err := codec.MarshalStable(payload)
	if err != nil {
		return RecordDraft{}, fmt.Errorf("marshal session payload: %w", err)
	}
	if len(encoded) == 0 || len(encoded) > MaxRecordBytes {
		return RecordDraft{}, fmt.Errorf("session payload exceeds size limit")
	}
	return RecordDraft{
		descriptor: descriptor, parentThreadID: parentThreadID, turnID: turnID,
		payload: append(json.RawMessage(nil), encoded...),
	}, nil
}

func (draft RecordDraft) validate() error {
	descriptor, exists := descriptorByKind(draft.descriptor.Kind)
	if !exists || descriptor != draft.descriptor {
		return fmt.Errorf("session draft descriptor is invalid")
	}
	if len(draft.payload) == 0 || len(draft.payload) > MaxRecordBytes || !codec.Valid(draft.payload) {
		return fmt.Errorf("session draft payload is invalid")
	}
	if err := validateDraftPlacement(draft); err != nil {
		return err
	}
	return validateDraftPayload(draft)
}

func (draft RecordDraft) clone() RecordDraft {
	draft.payload = draft.PayloadBytes()
	return draft
}

func validateDraftPlacement(draft RecordDraft) error {
	switch draft.descriptor.Kind {
	case EventSessionMeta:
		if draft.parentThreadID != "" || draft.turnID != "" {
			return fmt.Errorf("session metadata draft placement is invalid")
		}
	case EventThreadMeta:
		if draft.turnID != "" {
			return fmt.Errorf("thread metadata draft placement is invalid")
		}
	case EventTurnStarted, EventProviderNativeCommit, EventTurnCompleted, EventTurnFailed:
		if draft.parentThreadID != "" || !draft.turnID.Valid() {
			return fmt.Errorf("turn draft placement is invalid")
		}
	default:
		return fmt.Errorf("session draft event kind is invalid")
	}
	return nil
}

func validateDraftPayload(draft RecordDraft) error {
	record := Record{
		PayloadVersion: draft.descriptor.Version, ReplayRequirement: draft.descriptor.Requirement,
		EventKind: draft.descriptor.Kind, Payload: draft.PayloadBytes(),
	}
	switch draft.descriptor.Kind {
	case EventSessionMeta:
		_, err := DecodeSessionMetaPayload(record)
		return err
	case EventThreadMeta:
		payload, err := DecodeThreadMetaPayload(record)
		if err == nil && payload.ParentThreadID != draft.parentThreadID {
			return fmt.Errorf("thread metadata parent does not match draft")
		}
		return err
	case EventTurnStarted:
		_, err := DecodeTurnStartedPayload(record)
		return err
	case EventProviderNativeCommit:
		_, err := DecodeNativeCommitPayload(record)
		return err
	case EventTurnCompleted:
		_, err := DecodeTurnCompletedPayload(record)
		return err
	case EventTurnFailed:
		_, err := DecodeTurnFailedPayload(record)
		return err
	default:
		return fmt.Errorf("session draft event kind is invalid")
	}
}

func validateTurnID(turnID domain.TurnID) error {
	if !turnID.Valid() {
		return fmt.Errorf("session turn ID is invalid")
	}
	return nil
}
