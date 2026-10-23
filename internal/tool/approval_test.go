package tool

import (
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestPatchCandidateAndApprovalValuesAreBoundedAndImmutable(t *testing.T) {
	t.Parallel()

	invocation := testPatchInvocation(t)
	base := []byte("old\n")
	evidence, err := NewFileBaseEvidence(NewContentDigest(base), int64(len(base)), 0o644, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	metadata, _ := NewPatchResultMetadata("main.go", 1, 1, 1)
	finalBytes := []byte("new\n")
	fingerprint, _ := PatchFingerprint(invocation.Input(), evidence, finalBytes)
	candidate, err := NewPatchCandidate(invocation, evidence, finalBytes, "--- a/main.go\n+++ b/main.go\n-old\n+new\n", fingerprint, metadata)
	if err != nil {
		t.Fatalf("创建candidate: %v", err)
	}
	finalBytes[0] = 'x'
	copyOut := candidate.FinalBytes()
	copyOut[0] = 'x'
	if string(candidate.FinalBytes()) != "new\n" {
		t.Fatal("candidate泄漏final bytes")
	}
	if _, err := NewPatchCandidate(invocation, evidence, []byte("new\n"), candidate.Diff(), NewCandidateFingerprint([]byte("forged")), metadata); err == nil {
		t.Fatal("candidate接受了未绑定内容的fingerprint")
	}

	requestID, _ := ParseApprovalRequestID("018f1d8a-7b5c-7def-8123-456789abcdef")
	sessionID, _ := domain.ParseSessionID("018f1d8a-7b5c-7def-8123-456789abcdef")
	threadID, _ := domain.ParseThreadID("018f1d8a-7b5c-7def-8123-456789abcdef")
	turnID, _ := domain.ParseTurnID("018f1d8a-7b5c-7def-8123-456789abcdef")
	decisions := []ApprovalDecision{ApprovalAllowOnce, ApprovalAllowSession, ApprovalDeny}
	request, err := NewApprovalRequest(requestID, sessionID, threadID, turnID, candidate, "Patch modifies an existing workspace file.", decisions)
	if err != nil {
		t.Fatalf("创建approval request: %v", err)
	}
	decisions[0] = ApprovalAbort
	copyDecisions := request.Decisions()
	copyDecisions[0] = ApprovalAbort
	if request.Decisions()[0] != ApprovalAllowOnce || request.Fingerprint() != fingerprint || request.RelativePath() != "main.go" {
		t.Fatal("approval request未保持不可变绑定")
	}

	response, err := NewApprovalResponse(requestID, turnID, invocation.InvocationID(), fingerprint, ApprovalDeny, "  use another name  ")
	if err != nil || response.Feedback() != "use another name" {
		t.Fatalf("合法deny response错误: %#v %v", response, err)
	}
}

func TestApprovalValuesRejectInvalidIdentityDecisionAndFeedback(t *testing.T) {
	t.Parallel()

	requestID, _ := ParseApprovalRequestID("018f1d8a-7b5c-7def-8123-456789abcdef")
	turnID, _ := domain.ParseTurnID("018f1d8a-7b5c-7def-8123-456789abcdef")
	invocationID, _ := ParseInvocationID("018f1d8a-7b5c-7def-8123-456789abcdef")
	fingerprint := NewCandidateFingerprint([]byte("candidate"))
	if _, err := NewApprovalResponse(requestID, turnID, invocationID, fingerprint, ApprovalAllowOnce, "feedback"); err == nil {
		t.Fatal("allow决定不得携带反馈")
	}
	if _, err := NewApprovalResponse(requestID, turnID, invocationID, fingerprint, ApprovalDeny, strings.Repeat("x", MaxApprovalFeedbackBytes+1)); err == nil {
		t.Fatal("超限反馈应被拒绝")
	}
	if _, err := NewApprovalResponse(requestID, turnID, invocationID, fingerprint, ApprovalDecision("unknown"), ""); err == nil {
		t.Fatal("未知决定应被拒绝")
	}
	if _, err := ParseCandidateFingerprint(strings.Repeat("A", 64)); err == nil {
		t.Fatal("非小写hex fingerprint应被拒绝")
	}
	if _, err := NewPatchResultMetadata("/absolute/path", 1, 1, 1); err == nil {
		t.Fatal("绝对路径metadata应被拒绝")
	}
}

func testPatchInvocation(t *testing.T) PatchInvocation {
	t.Helper()
	replacement, _ := NewPatchReplacement("old\n", "new\n", false)
	input, _ := NewPatchInput("main.go", []PatchReplacement{replacement})
	callID, _ := ParseProviderCallID("call-patch")
	call, _ := NewPatchReadyCall(callID, WireCustomFreeform, input)
	invocationID, _ := ParseInvocationID("018f1d8a-7b5c-7def-8123-456789abcdef")
	invocation, err := NewPatchInvocation(invocationID, call)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}
