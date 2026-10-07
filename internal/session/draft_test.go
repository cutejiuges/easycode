package session

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/tool"
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
		{name: "sample usage", kind: EventSampleUsage, make: func() (RecordDraft, error) {
			return NewSampleUsageDraft(testTurnID, testSampleUsage(t))
		}},
		{name: "tool call ready", kind: EventToolCallReady, make: func() (RecordDraft, error) {
			return NewToolCallReadyDraft(testTurnID, testReadInvocation(t), 0, 0)
		}},
		{name: "tool execution started", kind: EventToolExecutionStarted, make: func() (RecordDraft, error) {
			return NewToolExecutionStartedDraft(testTurnID, testReadInvocation(t).InvocationID())
		}},
		{name: "tool call result", kind: EventToolCallResult, make: func() (RecordDraft, error) {
			return NewToolCallResultDraft(testTurnID, testReadResult(t))
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
		{name: "sample usage", make: func() error {
			_, err := NewSampleUsageDraft(testTurnID, domain.SampleUsage{})
			return err
		}},
		{name: "tool call ready index", make: func() error {
			_, err := NewToolCallReadyDraft(testTurnID, testReadInvocation(t), 16, 0)
			return err
		}},
		{name: "tool execution started", make: func() error {
			_, err := NewToolExecutionStartedDraft(testTurnID, "")
			return err
		}},
		{name: "tool call result", make: func() error {
			_, err := NewToolCallResultDraft(testTurnID, tool.InvocationResult{})
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
		mustDraft(t, EventSampleUsage, testTurnID, testSampleUsage(t)),
		mustToolReadyDraft(t),
		mustToolStartedDraft(t),
		mustToolResultDraft(t),
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

func TestToolLedgerPayloadsRoundTripAndRejectMalformedValues(t *testing.T) {
	readyRecord := Record{
		PayloadVersion: 1, ReplayRequirement: ReplayRequired, EventKind: EventToolCallReady,
		Payload: mustToolReadyDraft(t).PayloadBytes(),
	}
	readyPayload, err := DecodeToolCallReadyPayload(readyRecord)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := readyPayload.Domain()
	if err != nil || invocation.InvocationID() != testReadInvocation(t).InvocationID() || invocation.Input().FilePath() != "README.md" {
		t.Fatalf("ready payload domain = %#v, %v", invocation, err)
	}

	resultRecord := Record{
		PayloadVersion: 1, ReplayRequirement: ReplayRequired, EventKind: EventToolCallResult,
		Payload: mustToolResultDraft(t).PayloadBytes(),
	}
	resultPayload, err := DecodeToolCallResultPayload(resultRecord)
	if err != nil {
		t.Fatal(err)
	}
	result, err := resultPayload.Domain(invocation)
	if err != nil || result.Preview().Text() != "2\thello\n" || result.ProviderCallID() != invocation.ProviderCallID() {
		t.Fatalf("result payload domain = %#v, %v", result, err)
	}

	readyDuplicate := readyRecord
	readyDuplicate.Payload = bytes.Replace(readyRecord.Payload, []byte(`"sample_index":0`), []byte(`"sample_index":0,"sample_index":0`), 1)
	if _, err := DecodeToolCallReadyPayload(readyDuplicate); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate ready field error = %v", err)
	}
	resultDuplicate := resultRecord
	resultDuplicate.Payload = bytes.Replace(resultRecord.Payload, []byte(`"preview":`), []byte(`"preview":"duplicate","preview":`), 1)
	if _, err := DecodeToolCallResultPayload(resultDuplicate); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate result field error = %v", err)
	}

	invalidStatus := resultRecord
	invalidStatus.Payload = bytes.Replace(resultRecord.Payload, []byte(`"status":"success"`), []byte(`"status":"future"`), 1)
	if _, err := DecodeToolCallResultPayload(invalidStatus); err == nil {
		t.Fatal("invalid result status unexpectedly decoded")
	}
	oversized := resultRecord
	var payload ToolCallResultPayload
	if err := json.Unmarshal(resultRecord.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	payload.Preview = string(bytes.Repeat([]byte{'x'}, tool.MaxModelPreviewBytes+1))
	oversized.Payload, _ = json.Marshal(payload)
	if _, err := DecodeToolCallResultPayload(oversized); err == nil {
		t.Fatal("oversized result preview unexpectedly decoded")
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
	case EventSampleUsage:
		_, err := DecodeSampleUsagePayload(record)
		return err
	case EventToolCallReady:
		_, err := DecodeToolCallReadyPayload(record)
		return err
	case EventToolExecutionStarted:
		_, err := DecodeToolExecutionStartedPayload(record)
		return err
	case EventToolCallResult:
		_, err := DecodeToolCallResultPayload(record)
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

func testReadInvocation(t testing.TB) tool.ReadInvocation {
	t.Helper()
	invocationID, err := tool.ParseInvocationID("01890f3e-7bcd-7abc-8abc-0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	callID, err := tool.ParseProviderCallID("call-1")
	if err != nil {
		t.Fatal(err)
	}
	input, err := tool.NewReadInput("README.md", 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := tool.NewReadyCall(callID, input)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := tool.NewReadInvocation(invocationID, ready)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}

func testReadResult(t testing.TB) tool.InvocationResult {
	t.Helper()
	return tool.RenderReadSuccess(testReadInvocation(t), "README.md", []string{"hello"}, 1)
}

func mustToolReadyDraft(t testing.TB) RecordDraft {
	t.Helper()
	draft, err := NewToolCallReadyDraft(testTurnID, testReadInvocation(t), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

func mustToolStartedDraft(t testing.TB) RecordDraft {
	t.Helper()
	draft, err := NewToolExecutionStartedDraft(testTurnID, testReadInvocation(t).InvocationID())
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

func mustToolResultDraft(t testing.TB) RecordDraft {
	t.Helper()
	draft, err := NewToolCallResultDraft(testTurnID, testReadResult(t))
	if err != nil {
		t.Fatal(err)
	}
	return draft
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
	case EventSampleUsage:
		draft, err = NewSampleUsageDraft(turnID, payload.(domain.SampleUsage))
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

func testSampleUsage(t testing.TB) domain.SampleUsage {
	t.Helper()
	usage, err := domain.NewSampleUsage(
		domain.KnownUsageMetric(0), domain.KnownUsageMetric(2),
		domain.UnknownUsageMetric(), domain.KnownUsageMetric(3),
		domain.NotApplicableUsageMetric(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return usage
}

func TestSampleUsagePayloadPreservesStateAndValuePresence(t *testing.T) {
	t.Parallel()
	payload, err := NewSampleUsagePayload(testSampleUsage(t))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"input_uncached":{"state":"known","value":0},"cache_read":{"state":"known","value":2},"cache_write":{"state":"unknown"},"output":{"state":"known","value":3},"reasoning_output":{"state":"not_applicable"}}`
	if string(encoded) != want {
		t.Fatalf("sample usage payload = %s", encoded)
	}
	invalid := payload
	zero := uint64(0)
	invalid.CacheWrite.Value = &zero
	if _, err := invalid.Domain(); err == nil {
		t.Fatal("unknown metric with value unexpectedly accepted")
	}
}
