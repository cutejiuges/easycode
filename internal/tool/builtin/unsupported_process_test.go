package builtin

import (
	"context"
	"path/filepath"
	"testing"

	"easycode/internal/tool"
)

func TestUnsupportedLauncherProducesStableResultWithoutProcess(t *testing.T) {
	input, _ := tool.NewExecInput("printf forbidden > forbidden", "", 1000)
	ready, _ := tool.NewExecReadyCall("call-unsupported", input)
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	invocation, _ := tool.NewExecInvocation(id, ready)
	shell, err := tool.NewShellProfile("fixture-shell", filepath.Join(t.TempDir(), "absent-shell"))
	if err != nil {
		t.Fatal(err)
	}
	environment, _ := tool.NewEnvironmentPolicy("fixture", nil)
	sandbox, _ := tool.NewSandboxProfile("fixture", false, true)
	candidate, err := tool.NewExecCandidate(invocation, shell, environment, sandbox)
	if err != nil {
		t.Fatal(err)
	}
	executor := &ExecExecutor{launcher: unsupportedProcessLauncher{}, renderer: tool.RenderExecOutput}
	progressCalls := 0
	result := executor.Execute(context.Background(), candidate, func(tool.ExecProgress) { progressCalls++ })
	if result.Status() != tool.ResultError || result.Code() != "unsupported_platform" || result.Preview().Text() != "Exec failed: unsupported_platform" || progressCalls != 0 {
		t.Fatal("unsupported executor attempted a fallback")
	}
	if _, known := result.ExecMetadata().ExitCode(); known || result.ExecMetadata().Stdout().ObservedBytes() != 0 || result.ExecMetadata().Stderr().ObservedBytes() != 0 {
		t.Fatal("unsupported executor invented process output")
	}
}
