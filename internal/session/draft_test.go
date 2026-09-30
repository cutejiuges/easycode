package session

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
)

func TestTypedDraftConstructorsSealKindAndPayload(t *testing.T) {
	sessionPayload := SessionMetaPayload{
		RootThreadID: testThreadID, CreatedAt: time.Unix(1, 0).UTC(),
		Provider: domain.ProviderOpenAI, ProviderWire: "responses", Model: "gpt-test",
		SchemaRevision: 1, CreationCWD: t.TempDir(),
	}
	nativeBytes := json.RawMessage(`{"shape":"text_sample"}`)
	tests := []struct {
		name string
		kind EventKind
		make func() (RecordDraft, error)
	}{
		{name: "session metadata", kind: EventSessionMeta, make: func() (RecordDraft, error) {
			return NewSessionMetaDraft(sessionPayload)
		}},
		{name: "thread metadata", kind: EventThreadMeta, make: func() (RecordDraft, error) {
			return NewThreadMetaDraft(ThreadMetaPayload{Root: true})
		}},
		{name: "turn started", kind: EventTurnStarted, make: func() (RecordDraft, error) {
			return NewTurnStartedDraft(testTurnID)
		}},
		{name: "native commit", kind: EventProviderNativeCommit, make: func() (RecordDraft, error) {
			return NewProviderNativeCommitDraft(testTurnID, NativeCommitPayload{
				Provider: domain.ProviderOpenAI, Wire: "responses", PayloadVersion: 1, Payload: nativeBytes,
			})
		}},
		{name: "turn completed", kind: EventTurnCompleted, make: func() (RecordDraft, error) {
			return NewTurnCompletedDraft(testTurnID)
		}},
		{name: "turn failed", kind: EventTurnFailed, make: func() (RecordDraft, error) {
			return NewTurnFailedDraft(testTurnID, TurnFailedPayload{Code: "failed"})
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			draft, err := test.make()
			if err != nil {
				t.Fatalf("create draft: %v", err)
			}
			if draft.EventKind() != test.kind || draft.descriptor.Version != 1 || draft.descriptor.Requirement != ReplayRequired {
				t.Fatalf("draft declaration = %#v", draft)
			}
			first := draft.PayloadBytes()
			first[0] = '['
			if draft.PayloadBytes()[0] == '[' {
				t.Fatal("draft payload getter shares mutable bytes")
			}
		})
	}

	sealedNative, err := tests[3].make()
	if err != nil {
		t.Fatalf("create native draft before mutation: %v", err)
	}
	nativeBytes[0] = '['
	if !json.Valid(sealedNative.PayloadBytes()) {
		t.Fatal("caller mutation changed sealed native draft")
	}
	nativeDraft, err := tests[3].make()
	if err == nil || nativeDraft.EventKind() != "" {
		t.Fatalf("mutated native payload remained constructible: %#v, %v", nativeDraft, err)
	}
	if err := (RecordDraft{}).validate(); err == nil {
		t.Fatal("zero-value draft unexpectedly validated")
	}
}

func TestTypedDraftConstructorsRejectSemanticInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		make func() error
	}{
		{name: "session metadata", make: func() error {
			_, err := NewSessionMetaDraft(SessionMetaPayload{})
			return err
		}},
		{name: "thread metadata", make: func() error {
			_, err := NewThreadMetaDraft(ThreadMetaPayload{})
			return err
		}},
		{name: "turn started", make: func() error {
			_, err := NewTurnStartedDraft("")
			return err
		}},
		{name: "native commit", make: func() error {
			_, err := NewProviderNativeCommitDraft(testTurnID, NativeCommitPayload{})
			return err
		}},
		{name: "turn completed", make: func() error {
			_, err := NewTurnCompletedDraft("")
			return err
		}},
		{name: "turn failed", make: func() error {
			_, err := NewTurnFailedDraft(testTurnID, TurnFailedPayload{})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.make(); err == nil {
				t.Fatal("expected semantic validation error")
			}
		})
	}
}

func TestRevisionSpecificDecodersAreStrict(t *testing.T) {
	validDrafts := []RecordDraft{
		mustDraft(t, EventSessionMeta, "", SessionMetaPayload{
			RootThreadID: testThreadID, CreatedAt: time.Unix(1, 0).UTC(),
			Provider: domain.ProviderOpenAI, ProviderWire: "responses", Model: "gpt-test",
			SchemaRevision: 1, CreationCWD: t.TempDir(),
		}),
		mustDraft(t, EventThreadMeta, "", ThreadMetaPayload{Root: true}),
		mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}),
		mustDraft(t, EventProviderNativeCommit, testTurnID, NativeCommitPayload{
			Provider: domain.ProviderOpenAI, Wire: "responses", PayloadVersion: 1,
			Payload: json.RawMessage(`{"shape":"text_sample"}`),
		}),
		mustDraft(t, EventTurnCompleted, testTurnID, TurnCompletedPayload{}),
		mustDraft(t, EventTurnFailed, testTurnID, TurnFailedPayload{Code: "failed"}),
	}

	for _, draft := range validDrafts {
		record := Record{
			PayloadVersion: draft.descriptor.Version, ReplayRequirement: draft.descriptor.Requirement,
			EventKind: draft.EventKind(), Payload: draft.PayloadBytes(),
		}
		t.Run(string(draft.EventKind()), func(t *testing.T) {
			if err := decodeKnownRecord(record); err != nil {
				t.Fatalf("decode valid payload: %v", err)
			}

			unknown := record
			unknown.Payload = json.RawMessage(`{"unknown":true}`)
			if err := decodeKnownRecord(unknown); err == nil || !strings.Contains(err.Error(), "unknown") {
				t.Fatalf("unknown field error = %v", err)
			}
			trailing := record
			trailing.Payload = append(append(json.RawMessage(nil), trailing.Payload...), []byte(` {}`)...)
			if err := decodeKnownRecord(trailing); err == nil || !strings.Contains(err.Error(), "trailing") {
				t.Fatalf("trailing JSON error = %v", err)
			}
			wrongRevision := record
			wrongRevision.PayloadVersion++
			if err := decodeKnownRecord(wrongRevision); err == nil || !strings.Contains(err.Error(), "declaration") {
				t.Fatalf("revision error = %v", err)
			}
			wrongRequirement := record
			wrongRequirement.ReplayRequirement = ReplayOptional
			if err := decodeKnownRecord(wrongRequirement); err == nil || !strings.Contains(err.Error(), "declaration") {
				t.Fatalf("requirement error = %v", err)
			}
		})
	}
}

func decodeKnownRecord(record Record) error {
	switch record.EventKind {
	case EventSessionMeta:
		_, err := DecodeSessionMetaPayload(record)
		return err
	case EventThreadMeta:
		_, err := DecodeThreadMetaPayload(record)
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
		return nil
	}
}

func mustDraft(t testing.TB, kind EventKind, turnID domain.TurnID, payload any) RecordDraft {
	t.Helper()
	var (
		draft RecordDraft
		err   error
	)
	switch kind {
	case EventSessionMeta:
		draft, err = NewSessionMetaDraft(payload.(SessionMetaPayload))
	case EventThreadMeta:
		draft, err = NewThreadMetaDraft(payload.(ThreadMetaPayload))
	case EventTurnStarted:
		draft, err = NewTurnStartedDraft(turnID)
	case EventProviderNativeCommit:
		draft, err = NewProviderNativeCommitDraft(turnID, payload.(NativeCommitPayload))
	case EventTurnCompleted:
		draft, err = NewTurnCompletedDraft(turnID)
	case EventTurnFailed:
		draft, err = NewTurnFailedDraft(turnID, payload.(TurnFailedPayload))
	default:
		t.Fatalf("unsupported test draft kind %q", kind)
	}
	if err != nil {
		t.Fatalf("create %s draft: %v", kind, err)
	}
	return draft
}
