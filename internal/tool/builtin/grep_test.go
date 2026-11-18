//go:build darwin || linux

package builtin

import (
	"os"
	"path/filepath"
	"testing"

	"easycode/internal/tool"
)

func openTestGrepExecutor(t *testing.T, root string) *GrepExecutor {
	t.Helper()
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	executor, err := NewGrepExecutor(workspace)
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func grepInvocationFor(t *testing.T, pattern string, searchPath string, glob string, mode tool.GrepOutputMode, insensitive bool, before int, after int, limit int) tool.GrepInvocation {
	t.Helper()
	input, err := tool.NewGrepInput(pattern, searchPath, glob, mode, insensitive, before, after, limit)
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := tool.ParseProviderCallID("call-grep")
	call, _ := tool.NewGrepReadyCall(callID, input)
	invocationID, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8123-456789abcdef")
	invocation, err := tool.NewGrepInvocation(invocationID, call)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}

func writeSearchFile(t *testing.T, root string, relative string, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
