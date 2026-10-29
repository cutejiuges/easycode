package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"easycode/internal/codec"
)

func TestExecApprovalFingerprintBindsSavingPolicyAndEveryBudget(t *testing.T) {
	id, _ := GenerateInvocationID()
	invocation := execTestInvocation(t, "go test ./...", id)
	shell, _ := NewShellProfile("sh", "/bin/sh")
	environment, _ := NewEnvironmentPolicy("minimal", []string{"PATH"})
	sandbox, _ := NewSandboxProfile("workspace", false, true)
	candidate, err := NewExecCandidate(invocation, shell, environment, sandbox)
	if err != nil {
		t.Fatal(err)
	}
	input := invocation.Input()
	base := execCandidateCanonical{Capability: CapabilityExec, Command: input.Command(), Workdir: input.Workdir(), TimeoutMillis: input.TimeoutMillis(), ShellProfile: shell.ID(), EnvironmentRevision: environment.Revision(), SandboxRevision: sandbox.Revision(), NetworkAllowed: sandbox.NetworkAllowed(), WriteWorkspace: sandbox.WriteWorkspace(), CaptureBytes: ExecCaptureBytesPerStream, PreviewBytes: ExecModelPreviewBytes, SavingPolicy: "private-command-prefix", SavedStreamBytes: ArtifactStreamBytes, SavedInvocationBytes: ArtifactInvocationBytes, SavedThreadBytes: ArtifactThreadBytes, SavedRootBytes: ArtifactRootBytes}
	for _, change := range []func(*execCandidateCanonical){
		func(v *execCandidateCanonical) { v.SavingPolicy = "none" },
		func(v *execCandidateCanonical) { v.SavedStreamBytes++ },
		func(v *execCandidateCanonical) { v.SavedInvocationBytes++ },
		func(v *execCandidateCanonical) { v.SavedThreadBytes++ },
		func(v *execCandidateCanonical) { v.SavedRootBytes++ },
	} {
		modified := base
		change(&modified)
		encoded, err := codec.MarshalCanonical(modified, 256<<10)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(encoded.Bytes())
		stale := candidate
		stale.fingerprint = hex.EncodeToString(digest[:])
		if stale.Fingerprint() == candidate.Fingerprint() || stale.Validate() == nil {
			t.Fatal("changed saving policy accepted old approval fingerprint")
		}
	}
}
