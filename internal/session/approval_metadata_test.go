package session

import (
	"strings"
	"testing"

	"easycode/internal/tool"
)

func TestApprovalFactsHaveStrictSafeMetadataAndDecisionSource(t *testing.T) {
	invocation := replayExecInvocation(t)
	metadata, err := NewApprovalMetadata(invocation)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := tool.ParseApprovalRequestID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	for _, test := range []struct {
		source  ApprovalDecisionSource
		outcome tool.ApprovalOutcome
	}{
		{ApprovalSourceInteractive, tool.ApprovalOutcomeAllowOnce},
		{ApprovalSourceHeadless, tool.ApprovalOutcomeDeny},
		{ApprovalSourceInterruption, tool.ApprovalOutcomeAbort},
	} {
		fact := ToolApprovalDecidedPayload{RequestID: id, InvocationID: invocation.InvocationID(), Fingerprint: tool.NewCandidateFingerprint([]byte("candidate")), Outcome: test.outcome, Source: test.source, Metadata: metadata}
		draft, err := NewToolApprovalDecidedDraft(testTurnID, fact)
		if err != nil {
			t.Fatal(err)
		}
		raw := string(draft.PayloadBytes())
		record := Record{PayloadVersion: EnvelopeVersion, EventKind: EventToolApprovalDecided, Payload: draft.PayloadBytes()}
		decoded, err := DecodeToolApprovalDecidedPayload(record)
		if err != nil || decoded != fact {
			t.Fatalf("approval fact changed: %v", err)
		}
		if strings.Contains(raw, "printf hello") || strings.Contains(raw, "environment") {
			t.Fatal("approval stored executable content")
		}
		mutations := []string{
			strings.Replace(raw, `"decision_source":"`+string(test.source)+`",`, "", 1),
			strings.Replace(raw, `"decision_source":"`+string(test.source)+`"`, `"decision_source":"private-secret"`, 1),
			strings.Replace(raw, `"relative_path":"src"`, `"relative_path":"/private/secret"`, 1),
			strings.Replace(raw, `"timeout_ms":1000`, `"timeout_ms":0`, 1),
			raw + `{}`,
		}
		for _, field := range []string{`"pid":42,`, `"command":"private-secret",`, `"environment":"private-secret",`, `"output_chunks":["private-secret"],`, `"cause":"private-secret",`} {
			mutations = append(mutations, strings.Replace(raw, `"metadata":{`, `"metadata":{`+field, 1))
		}
		for _, mutated := range mutations {
			if mutated == raw {
				t.Fatal("mutation did not change fixture")
			}
			record.Payload = []byte(mutated)
			if _, err := DecodeToolApprovalDecidedPayload(record); err == nil || strings.Contains(err.Error(), "private-secret") {
				t.Fatal("invalid approval accepted or leaked content")
			}
		}
		fact.Source = ""
		if _, err := NewToolApprovalDecidedDraft(testTurnID, fact); err == nil {
			t.Fatal("missing source accepted")
		}
	}
}

func TestReplayRejectsApprovalMetadataNotMatchingInvocation(t *testing.T) {
	fixture := newReplayFixture(t)
	fixture.appendMetadata()
	fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
	invocation := replayExecInvocation(t)
	appendReplayCallSample(t, fixture, invocation)
	draft := mustPatchApprovalDraft(t, invocation, tool.NewCandidateFingerprint([]byte("candidate")), tool.ApprovalOutcomeAllowOnce)
	payload, err := DecodeToolApprovalDecidedPayload(Record{PayloadVersion: EnvelopeVersion, EventKind: EventToolApprovalDecided, Payload: draft.PayloadBytes()})
	if err != nil {
		t.Fatal(err)
	}
	payload.Metadata.RelativePath = "different-workdir"
	draft, err = NewToolApprovalDecidedDraft(testTurnID, payload)
	if err != nil {
		t.Fatal(err)
	}
	fixture.appendBatch(draft)
	if _, err := NewReplayPlanner().Plan(fixture.loaded()); err == nil {
		t.Fatal("mismatched approval metadata accepted")
	}
}
