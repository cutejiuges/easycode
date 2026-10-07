package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/tool"
)

type inertWorkspaceHandle struct{ opened int }

func (handle *inertWorkspaceHandle) openFile(string, workspaceHooks) (*os.File, error) {
	handle.opened++
	return nil, errors.New("not used")
}
func (*inertWorkspaceHandle) close() error { return nil }

func TestNewReadExecutorDoesNotAccessFilesystem(t *testing.T) {
	t.Parallel()
	handle := &inertWorkspaceHandle{}
	workspace := &Workspace{rootPath: "/not/opened", handle: handle}
	if _, err := NewReadExecutor(workspace); err != nil {
		t.Fatalf("纯构造失败: %v", err)
	}
	if handle.opened != 0 {
		t.Fatal("NewReadExecutor 执行了文件 I/O")
	}
}

func TestReadWorkspaceTextAndRange(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "src", "main.go")
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("one\r\ntwo\nthree\nfour"), 0o600); err != nil {
		t.Fatal(err)
	}
	executor, workspace := openTestExecutor(t, root)
	result := executor.Execute(context.Background(), invocationFor(t, filepath.Join(root, "src", "main.go"), 2, 2))
	if result.Status() != tool.ResultSuccess || result.Code() != "ok" {
		t.Fatalf("Read 失败: %s %s", result.Status(), result.Code())
	}
	if result.Preview().Text() != "2\ttwo\n3\tthree\n" || result.Metadata().RelativePath() != "src/main.go" {
		t.Fatalf("范围或路径错误: %q %#v", result.Preview().Text(), result.Metadata())
	}
	if result.Metadata().ReachedEOF() {
		t.Fatal("读取中间范围不应到达 EOF")
	}
	if err := workspace.Close(); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Close(); err != nil {
		t.Fatalf("Close 必须幂等: %v", err)
	}
}

func TestReadEmptyMissingNewlineAndPastEOF(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "empty"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tail"), []byte("a\nb"), 0o600); err != nil {
		t.Fatal(err)
	}
	executor, _ := openTestExecutor(t, root)
	empty := executor.Execute(context.Background(), invocationFor(t, "empty", 1, 2000))
	if empty.Preview().Text() != "" || !empty.Metadata().ReachedEOF() {
		t.Fatalf("空文件结果错误: %q %#v", empty.Preview().Text(), empty.Metadata())
	}
	tail := executor.Execute(context.Background(), invocationFor(t, "tail", 2, 1))
	if tail.Preview().Text() != "2\tb\n" || !tail.Metadata().ReachedEOF() {
		t.Fatalf("无末尾换行结果错误: %q %#v", tail.Preview().Text(), tail.Metadata())
	}
	past := executor.Execute(context.Background(), invocationFor(t, "tail", 9, 1))
	if past.Preview().Text() != "" || !past.Metadata().ReachedEOF() {
		t.Fatalf("超出 EOF 结果错误: %q %#v", past.Preview().Text(), past.Metadata())
	}
}

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
		result := executor.Execute(context.Background(), invocationFor(t, test.path, 1, 20))
		if result.Status() != tool.ResultError || result.Code() != test.code {
			t.Fatalf("%q = %s/%s", test.path, result.Status(), result.Code())
		}
		combined := result.Preview().Text() + result.Metadata().RelativePath()
		if strings.Contains(combined, root) || strings.Contains(combined, outside) || strings.Contains(combined, secret) {
			t.Fatalf("安全错误泄漏绝对路径或正文: %q", combined)
		}
	}
}

func TestReadCancellationBeforeAdmissionDoesNotOpen(t *testing.T) {
	t.Parallel()
	handle := &inertWorkspaceHandle{}
	executor, err := NewReadExecutor(&Workspace{rootPath: "/workspace", handle: handle})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := executor.Execute(ctx, invocationFor(t, "file", 1, 1))
	if result.Status() != tool.ResultCancelled || handle.opened != 0 {
		t.Fatalf("取消前置仍执行 I/O: status=%s opens=%d", result.Status(), handle.opened)
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
	call, _ := tool.NewReadyCall(callID, input)
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
