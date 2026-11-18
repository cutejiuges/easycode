package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/tool"
)

func TestReadRejectsTraversalSymlinkBinaryAndOversize(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	secret := "outside-secret-value"
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nul"), []byte{'a', 0, 'b'}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "invalid"), []byte{0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	large, err := os.OpenFile(filepath.Join(root, "large"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := large.Truncate(maxReadFileBytes + 1); err != nil {
		_ = large.Close()
		t.Fatal(err)
	}
	if err := large.Close(); err != nil {
		t.Fatal(err)
	}
	executor, _ := openTestExecutor(t, root)
	tests := []struct {
		path string
		code string
	}{
		{path: "../secret", code: "path_outside_workspace"},
		{path: filepath.Join(outside, "secret"), code: "path_outside_workspace"},
		{path: "link", code: "file_unavailable"},
		{path: "nul", code: "binary_file"},
		{path: "invalid", code: "binary_file"},
		{path: "large", code: "file_too_large"},
	}
	for _, test := range tests {
		result := executor.Execute(context.Background(), invocationFor(t, test.path, 1, 20)).Result()
		if result.Status() != tool.ResultError || result.Code() != test.code {
			t.Fatalf("%q = %s/%s", test.path, result.Status(), result.Code())
		}
		combined := result.Preview().Text() + result.ReadMetadata().RelativePath()
		if strings.Contains(combined, root) || strings.Contains(combined, outside) || strings.Contains(combined, secret) {
			t.Fatalf("安全错误泄漏绝对路径或正文: %q", combined)
		}
	}
}

func openTestExecutor(t *testing.T, root string) (*ReadExecutor, *Workspace) {
	t.Helper()
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	executor, err := NewReadExecutor(workspace)
	if err != nil {
		t.Fatal(err)
	}
	return executor, workspace
}

func invocationFor(t *testing.T, path string, offset int, limit int) tool.ReadInvocation {
	t.Helper()
	input, err := tool.NewReadInput(path, offset, limit)
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := tool.ParseProviderCallID("call-read")
	call, _ := tool.NewReadReadyCall(callID, input)
	invocationID, err := tool.ParseInvocationID("018f1d8a-7b5c-7def-8123-456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := tool.NewReadInvocation(invocationID, call)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}
