package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/tool"
)

type artifactBoundaryExecutor struct{ calls int }

func (e *artifactBoundaryExecutor) Execute(_ context.Context, c tool.ExecCandidate, _ func(tool.ExecProgress)) tool.InvocationResult {
	e.calls++
	return tool.NewExecErrorResult(c.Invocation(), tool.ResultError, "test_executor", "Test executor")
}

func TestCommandArtifactBoundaryRejectsNestedStorage(t *testing.T) {
	workspace := privateAppTempDir(t)
	storage := filepath.Join(workspace, "sessions")
	if err := os.Mkdir(storage, 0o700); err != nil {
		t.Fatal(err)
	}
	if l, err := openCommandArtifactBoundary(context.Background(), storage, workspace); err == nil {
		_ = l.Close()
		t.Fatal("nested storage accepted")
	} else if !strings.Contains(err.Error(), "unsafe_storage_layout") || strings.Contains(err.Error(), storage) {
		t.Fatal("unsafe layout error leaked path")
	}
}

func TestCommandArtifactBoundaryRejectsPrivateAncestorAlias(t *testing.T) {
	workspace, parent, target := privateAppTempDir(t), privateAppTempDir(t), privateAppTempDir(t)
	if err := os.Mkdir(filepath.Join(target, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	if layout, err := openCommandArtifactBoundary(context.Background(), filepath.Join(alias, "sessions"), workspace); err == nil {
		_ = layout.Close()
		t.Fatal("private ancestor symlink accepted")
	}
}

func TestCommandArtifactBoundaryRechecksBeforeDelegation(t *testing.T) {
	workspace, storage := privateAppTempDir(t), privateAppTempDir(t)
	l, err := openCommandArtifactBoundary(context.Background(), storage, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	input, _ := tool.NewExecInput("true", "", 1000)
	call, _ := tool.NewExecReadyCall("boundary-test", input)
	id, _ := tool.GenerateInvocationID()
	inv, _ := tool.NewExecInvocation(id, call)
	shell, _ := tool.NewShellProfile("posix-sh-nonlogin", "/bin/sh")
	env, _ := tool.NewEnvironmentPolicy("minimal-command-environment", []string{"PATH"})
	sandbox, _ := tool.NewSandboxProfile("darwin-workspace-offline", false, true)
	candidate, _ := tool.NewExecCandidate(inv, shell, env, sandbox)
	executor := &artifactBoundaryExecutor{}
	boundary := &commandArtifactBoundary{layout: l, executor: executor}
	boundary.Execute(context.Background(), candidate, nil)
	if executor.calls != 1 {
		t.Fatal("safe execution rejected")
	}
	if err = os.Rename(storage, storage+"-moved"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(storage + "-moved") }()
	if err = os.Mkdir(storage, 0o700); err != nil {
		t.Fatal(err)
	}
	result := boundary.Execute(context.Background(), candidate, nil)
	if executor.calls != 1 || result.Code() != "unsafe_storage_layout" {
		t.Fatal("changed layout reached executor")
	}
}
