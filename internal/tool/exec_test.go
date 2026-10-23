package tool

import (
	"strings"
	"testing"
)

func TestExecInputStrictDecode(t *testing.T) {
	for _, raw := range []string{
		`{}`, `{"command":""}`, `{"command":null}`, `{"command":"true","workdir":null}`,
		`{"command":"true","timeout_ms":null}`, `{"command":"true","timeout_ms":0}`,
		`{"command":"true","timeout_ms":999}`, `{"command":"true","timeout_ms":600001}`,
		`{"command":"true","timeout_ms":1000.5}`, `{"command":"true","timeout_ms":"1000"}`,
		`{"command":"true","command":"false"}`, `{"command":"true","tty":false}`,
		`{"command":"true"} {}`, `[]`, `{"command":"true","workdir":"../outside"}`,
		`{"command":"true","workdir":"/tmp"}`, `{"command":"true","workdir":"a/../b"}`,
		`{"command":"true","workdir":"a\\b"}`, `{"command":"a\u0000b"}`,
		"{\"command\":\"\xff\"}",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := DecodeExecInput([]byte(raw)); err == nil {
				t.Fatal("invalid command input accepted")
			}
		})
	}
	input, err := DecodeExecInput([]byte(`{"command":"printf '你好'","workdir":"src"}`))
	if err != nil || input.Command() != "printf '你好'" || input.Workdir() != "src" || input.TimeoutMillis() != DefaultExecTimeoutMillis {
		t.Fatalf("decoded input = %#v, %v", input, err)
	}
	for _, timeout := range []int{MinExecTimeoutMillis, MaxExecTimeoutMillis} {
		if _, err := NewExecInput(strings.Repeat("x", MaxExecCommandBytes), "", timeout); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewExecInput(strings.Repeat("x", MaxExecCommandBytes+1), "", 0); err == nil {
		t.Fatal("oversized command accepted")
	}
}

func execTestInvocation(t *testing.T, command string, id InvocationID) ExecInvocation {
	t.Helper()
	input, err := NewExecInput(command, "src", 0)
	if err != nil {
		t.Fatal(err)
	}
	call, err := NewExecReadyCall("call_exec", input)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := NewExecInvocation(id, call)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}

func TestExecCandidateFingerprintAndOwnership(t *testing.T) {
	id, err := ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	invocation := execTestInvocation(t, "go test ./...", id)
	shell, _ := NewShellProfile("sh", "/bin/sh")
	names := []string{"PATH", "LANG"}
	environment, _ := NewEnvironmentPolicy("minimal", names)
	sandbox, _ := NewSandboxProfile("workspace", false, true)
	candidate, err := NewExecCandidate(invocation, shell, environment, sandbox)
	if err != nil {
		t.Fatal(err)
	}
	names[0] = "SECRET"
	environment.allowed[0] = "CHANGED"
	copy := candidate.Environment()
	copy.allowed[0] = "MUTATED"
	returned := candidate.Environment().AllowedNames()
	returned[0] = "MUTATED"
	if candidate.Validate() != nil || candidate.Environment().AllowedNames()[0] != "LANG" {
		t.Fatal("candidate aliases mutable environment")
	}
	otherID, _ := ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdee")
	other, err := NewExecCandidate(execTestInvocation(t, "go test ./...", otherID), shell, candidate.Environment(), sandbox)
	if err != nil || candidate.Fingerprint() != other.Fingerprint() {
		t.Fatal("identity changed candidate fingerprint")
	}
	changed, err := NewExecCandidate(execTestInvocation(t, "go test ./internal/...", id), shell, candidate.Environment(), sandbox)
	if err != nil || candidate.Fingerprint() == changed.Fingerprint() {
		t.Fatal("command did not change candidate fingerprint")
	}
	changed.fingerprint = candidate.Fingerprint()
	if changed.Validate() == nil {
		t.Fatal("tampered candidate accepted")
	}
	for _, change := range []struct {
		command, workdir string
		timeout          int
		shell            ShellProfile
		environment      EnvironmentPolicy
		sandbox          SandboxProfile
	}{
		{"false", "src", DefaultExecTimeoutMillis, shell, candidate.Environment(), sandbox},
		{"go test ./...", "test", DefaultExecTimeoutMillis, shell, candidate.Environment(), sandbox},
		{"go test ./...", "src", MinExecTimeoutMillis, shell, candidate.Environment(), sandbox},
		{"go test ./...", "src", DefaultExecTimeoutMillis, ShellProfile{id: "bash", executable: "/bin/bash"}, candidate.Environment(), sandbox},
		{"go test ./...", "src", DefaultExecTimeoutMillis, shell, EnvironmentPolicy{revision: "other"}, sandbox},
		{"go test ./...", "src", DefaultExecTimeoutMillis, shell, candidate.Environment(), SandboxProfile{revision: "other", writeWorkspace: true}},
	} {
		input, err := NewExecInput(change.command, change.workdir, change.timeout)
		if err != nil {
			t.Fatal(err)
		}
		call, _ := NewExecReadyCall("call_exec", input)
		value, _ := NewExecInvocation(id, call)
		changed, err := NewExecCandidate(value, change.shell, change.environment, change.sandbox)
		if err != nil || changed.Fingerprint() == candidate.Fingerprint() {
			t.Fatalf("candidate change failed: %v", err)
		}
	}
}

func TestExecInvocationRejectsMismatchedUnion(t *testing.T) {
	id, _ := ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	exec := execTestInvocation(t, "true", id)
	call, _ := NewExecReadyCall(exec.ProviderCallID(), exec.Input())
	invocation, err := NewInvocation(id, call)
	if err != nil || invocation.Validate() != nil || invocation.InvocationID() != id || invocation.ProviderCallID() != exec.ProviderCallID() {
		t.Fatal("valid Exec invocation rejected")
	}
	call.readInput = ReadInput{filePath: "README.md", offset: 1, limit: 1}
	if call.Validate() == nil {
		t.Fatal("mixed ready payload accepted")
	}
	invocation.capability = CapabilityRead
	if invocation.Validate() == nil {
		t.Fatal("mixed invocation accepted")
	}
}

func TestExecResultTerminalValidation(t *testing.T) {
	id, _ := ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	invocation := execTestInvocation(t, "true", id)
	preview, _ := NewModelPreview("completed")
	for _, exit := range []int{0, 2, 255} {
		metadata, err := NewExecResultMetadata("src", exit, true, 100, ExecStreamStats{}, ExecStreamStats{}, false, false, false)
		if err != nil {
			t.Fatal(err)
		}
		result, err := NewExecInvocationResult(invocation, ResultSuccess, "ok", preview, metadata)
		if err != nil || result.Validate() != nil {
			t.Fatalf("completed exit %d rejected: %v", exit, err)
		}
	}
	for _, metadata := range []ExecResultMetadata{
		{workdir: "/tmp"}, {workdir: "../outside"}, {durationMillis: -1},
		{exitCode: 2}, {exitKnown: true, exitCode: 256}, {timedOut: true, cancelled: true},
		{stdout: ExecStreamStats{observed: 1, retained: 2}},
		{stdout: ExecStreamStats{observed: ExecCaptureBytesPerStream + 1, retained: ExecCaptureBytesPerStream + 1}},
	} {
		if metadata.Validate() == nil {
			t.Fatalf("invalid metadata accepted: %#v", metadata)
		}
	}
	if _, err := NewExecInvocationResult(invocation, ResultSuccess, "ok", preview, ExecResultMetadata{}); err == nil {
		t.Fatal("unknown exit accepted as success")
	}
	for _, status := range []ResultStatus{ResultDenied, ResultCancelled, ResultError, ResultOutcomeUncertain} {
		result := NewExecErrorResult(invocation, status, "execution_failed", "Exec failed")
		if result.Validate() != nil || result.Status() != status {
			t.Fatalf("error result lost status %s", status)
		}
	}
	result := NewExecErrorResult(invocation, ResultError, "timed_out", "Exec timed out")
	if result.Validate() != nil || !result.ExecMetadata().TimedOut() {
		t.Fatal("timeout result is invalid")
	}
	result.readMetadata = ReadResultMetadata{relativePath: "README.md"}
	if result.Validate() == nil {
		t.Fatal("mixed result metadata accepted")
	}
}

func TestExecResultRejectsInvalidStatesAndOversizedPreview(t *testing.T) {
	id, _ := ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	invocation := execTestInvocation(t, "true", id)
	preview, _ := NewModelPreview("completed")
	for _, test := range []struct {
		status   ResultStatus
		code     string
		metadata ExecResultMetadata
	}{
		{ResultSuccess, "ok", ExecResultMetadata{exitKnown: true, timedOut: true}},
		{ResultSuccess, "ok", ExecResultMetadata{exitKnown: true, cancelled: true}},
		{ResultDenied, "approval_denied", ExecResultMetadata{exitKnown: true}},
		{ResultDenied, "approval_denied", ExecResultMetadata{stdout: ExecStreamStats{observed: 1, retained: 1}}},
		{ResultError, "timed_out", ExecResultMetadata{}},
		{ResultCancelled, "cancelled", ExecResultMetadata{}},
		{ResultConflict, "conflict", ExecResultMetadata{}},
	} {
		if _, err := NewExecInvocationResult(invocation, test.status, test.code, preview, test.metadata); err == nil {
			t.Fatalf("invalid terminal accepted: %s %s", test.status, test.code)
		}
	}
	oversized := ModelPreview{text: strings.Repeat("x", MaxModelPreviewBytes+1)}
	if _, err := NewExecInvocationResult(invocation, ResultError, "execution_failed", oversized, ExecResultMetadata{}); err == nil {
		t.Fatal("oversized Exec preview accepted")
	}
	if !CapabilityExec.Exclusive() || !CapabilityPatch.Exclusive() || CapabilityRead.Exclusive() {
		t.Fatal("exclusive scheduling classification changed")
	}
}
