package protocol

import (
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestApprovalEventAndCommandStrictIdentity(t *testing.T) {
	payload := ToolApprovalRequestedPayload{RequestID: "018f1d8a-7b5c-7def-8123-456789abcdef", InvocationID: "018f1d8a-7b5c-7def-8abc-456789abcdef", ProviderCallID: "call-patch", Capability: "fs.patch", Reason: "Patch modifies an existing workspace file.", RelativePath: "main.go", Fingerprint: strings.Repeat("a", 64), Presentation: ApprovalPresentation{Kind: PresentationPatch, Patch: &PatchApprovalPresentation{Diff: "--- a/main.go\n+++ b/main.go\n-old\n+new\n"}}, Decisions: []ApprovalDecision{ApprovalAllowOnce, ApprovalAllowSession, ApprovalDeny}}
	event, err := NewToolApprovalRequested(payload)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeToolApprovalRequested(event)
	if err != nil || decoded.RelativePath != "main.go" {
		t.Fatalf("decoded=%#v error=%v", decoded, err)
	}
	decoded.Decisions[0] = ApprovalAbort
	again, _ := DecodeToolApprovalRequested(event)
	if again.Decisions[0] != ApprovalAllowOnce {
		t.Fatal("event payload共享decision slice")
	}
	requestID, _ := ParseRequestID("command-1")
	turnID, _ := domain.ParseTurnID("018f1d8a-7b5c-7def-8123-456789abcdef")
	command, err := NewResolveToolApprovalCommand(requestID, turnID, payload.RequestID, payload.InvocationID, payload.Fingerprint, ApprovalDeny, "use another name")
	if err != nil || command.Feedback() != "use another name" {
		t.Fatalf("command=%#v error=%v", command, err)
	}
	if _, err := NewResolveToolApprovalCommand(requestID, turnID, payload.RequestID, payload.InvocationID, payload.Fingerprint, ApprovalAllowOnce, "feedback"); err == nil {
		t.Fatal("allow command携带反馈未被拒绝")
	}
	payload.Presentation.Patch.Diff = strings.Repeat("x", maxApprovalDiffBytes+1)
	if _, err := NewToolApprovalRequested(payload); err == nil {
		t.Fatal("超限diff未被拒绝")
	}
}
