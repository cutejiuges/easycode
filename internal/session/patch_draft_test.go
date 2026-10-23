package session

import (
	"encoding/json"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

func sessionPatchInvocation(t *testing.T) tool.Invocation {
	t.Helper()
	replacement, err := tool.NewPatchLineReplacement([]string{"old"}, []string{"new"})
	if err != nil {
		t.Fatal(err)
	}
	input, err := tool.NewPatchInput("main.go", []tool.PatchReplacement{replacement})
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := tool.ParseProviderCallID("call-patch")
	ready, err := tool.NewPatchReadyCall(callID, tool.WireCustomFreeform, input)
	if err != nil {
		t.Fatal(err)
	}
	invocationID, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8123-456789abcdef")
	invocation, err := tool.NewInvocation(invocationID, ready)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}

func TestPatchReadyApprovalAndResultDraftRoundTrip(t *testing.T) {
	turnID, _ := domain.ParseTurnID("018f1d8a-7b5c-7def-8123-456789abcdef")
	invocation := sessionPatchInvocation(t)
	readyDraft, err := NewToolCallReadyDraft(turnID, invocation, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	readyPayload, err := DecodeToolCallReadyPayload(Record{PayloadVersion: EnvelopeVersion, EventKind: EventToolCallReady, Payload: readyDraft.PayloadBytes()})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := readyPayload.Domain()
	if err != nil {
		t.Fatal(err)
	}
	patch, _ := restored.Patch()
	if restored.WireKind() != tool.WireCustomFreeform || patch.Input().FilePath() != "main.go" || !patch.Input().Replacements()[0].LineBased() {
		t.Fatalf("restored=%#v", restored)
	}
	requestID, _ := tool.ParseApprovalRequestID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	fingerprint := tool.NewCandidateFingerprint([]byte("candidate"))
	approvalDraft, err := NewToolApprovalDecidedDraft(turnID, ToolApprovalDecidedPayload{Source: ApprovalSourceInteractive, Metadata: ApprovalMetadataPayload{Capability: tool.CapabilityPatch, RelativePath: "main.go"}, RequestID: requestID, InvocationID: invocation.InvocationID(), Fingerprint: fingerprint, Outcome: tool.ApprovalOutcomeDeny, Feedback: "use another name"})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := DecodeToolApprovalDecidedPayload(Record{PayloadVersion: EnvelopeVersion, EventKind: EventToolApprovalDecided, Payload: approvalDraft.PayloadBytes()})
	if err != nil || approval.Feedback != "use another name" {
		t.Fatalf("approval=%#v error=%v", approval, err)
	}
	patchInvocation, _ := invocation.Patch()
	result := tool.NewPatchErrorResult(patchInvocation, tool.ResultDenied, "approval_denied", "Patch denied: approval_denied")
	resultDraft, err := NewToolCallResultDraft(turnID, result)
	if err != nil {
		t.Fatal(err)
	}
	resultPayload, err := DecodeToolCallResultPayload(Record{PayloadVersion: EnvelopeVersion, EventKind: EventToolCallResult, Payload: resultDraft.PayloadBytes()})
	if err != nil {
		t.Fatal(err)
	}
	restoredResult, err := resultPayload.Domain(invocation)
	if err != nil || restoredResult.WireKind() != tool.WireCustomFreeform || restoredResult.Status() != tool.ResultDenied {
		t.Fatalf("result=%#v error=%v", restoredResult, err)
	}
}

func TestPatchSessionPayloadsFailClosed(t *testing.T) {
	turnID, _ := domain.ParseTurnID("018f1d8a-7b5c-7def-8123-456789abcdef")
	invocation := sessionPatchInvocation(t)
	draft, _ := NewToolCallReadyDraft(turnID, invocation, 0, 0)
	var object map[string]json.RawMessage
	if err := json.Unmarshal(draft.PayloadBytes(), &object); err != nil {
		t.Fatal(err)
	}
	object["future"] = json.RawMessage("true")
	encoded, _ := json.Marshal(object)
	if _, err := DecodeToolCallReadyPayload(Record{PayloadVersion: EnvelopeVersion, EventKind: EventToolCallReady, Payload: encoded}); err == nil {
		t.Fatal("未知Patch ready字段未被拒绝")
	}
	requestID, _ := tool.ParseApprovalRequestID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	fingerprint := tool.NewCandidateFingerprint([]byte("candidate"))
	if _, err := NewToolApprovalDecidedDraft(turnID, ToolApprovalDecidedPayload{Source: ApprovalSourceInteractive, Metadata: ApprovalMetadataPayload{Capability: tool.CapabilityPatch, RelativePath: "main.go"}, RequestID: requestID, InvocationID: invocation.InvocationID(), Fingerprint: fingerprint, Outcome: tool.ApprovalOutcomeAllowOnce, Feedback: "not allowed"}); err == nil {
		t.Fatal("allow approval携带反馈未被拒绝")
	}
	if _, err := NewToolApprovalDecidedDraft(turnID, ToolApprovalDecidedPayload{Source: ApprovalSourceInteractive, Metadata: ApprovalMetadataPayload{Capability: tool.CapabilityPatch, RelativePath: "main.go"}, RequestID: requestID, InvocationID: invocation.InvocationID(), Fingerprint: fingerprint, Outcome: tool.ApprovalOutcomeDeny, Feedback: strings.Repeat("x", tool.MaxApprovalFeedbackBytes+1)}); err == nil {
		t.Fatal("超限反馈未被拒绝")
	}
}
