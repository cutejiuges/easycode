package builtin

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"easycode/internal/tool"
)

func TestGlobReturnsSortedRegularFilesAndMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"internal/z.go", "internal/a.go", "internal/nested/b.go", "internal/readme.md", ".config/tool.go"} {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	executor, _ := openTestGlobExecutor(t, root)
	result := executor.Execute(context.Background(), globInvocationFor(t, "internal/**/*.go", "", 2))
	if result.Status() != tool.ResultSuccess || result.Capability() != tool.CapabilityGlob {
		t.Fatalf("Glob 失败: %s/%s", result.Status(), result.Code())
	}
	want := []string{"internal/a.go", "internal/nested/b.go"}
	if got := result.GlobMetadata().Matches(); !reflect.DeepEqual(got, want) {
		t.Fatalf("matches = %#v", got)
	}
	if !result.GlobMetadata().Truncated() || result.GlobMetadata().OmittedMatches() != 1 {
		t.Fatalf("截断元数据错误: %#v", result.GlobMetadata())
	}
	if result.Preview().Text() != "internal/a.go\ninternal/nested/b.go\n" {
		t.Fatalf("preview = %q", result.Preview().Text())
	}
}

func TestGlobUsesSubdirectoryButReturnsWorkspaceRelativePaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "nested", "main.go"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	executor, _ := openTestGlobExecutor(t, root)
	result := executor.Execute(context.Background(), globInvocationFor(t, "src/**/*.go", "src", 100))
	if got := result.GlobMetadata().Matches(); !reflect.DeepEqual(got, []string{"src/nested/main.go"}) {
		t.Fatalf("matches = %#v", got)
	}
}

func TestGlobCancellationBeforeTraversal(t *testing.T) {
	t.Parallel()
	executor, _ := openTestGlobExecutor(t, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := executor.Execute(ctx, globInvocationFor(t, "**", "", 100))
	if result.Status() != tool.ResultCancelled || result.Code() != "cancelled" {
		t.Fatalf("cancel = %s/%s", result.Status(), result.Code())
	}
}

func openTestGlobExecutor(t *testing.T, root string) (*GlobExecutor, *Workspace) {
	t.Helper()
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	executor, err := NewGlobExecutor(workspace)
	if err != nil {
		t.Fatal(err)
	}
	return executor, workspace
}

func globInvocationFor(t *testing.T, pattern string, searchPath string, limit int) tool.GlobInvocation {
	t.Helper()
	input, err := tool.NewGlobInput(pattern, searchPath, limit)
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := tool.ParseProviderCallID("call-glob")
	call, _ := tool.NewGlobReadyCall(callID, input)
	invocationID, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8123-456789abcdef")
	invocation, err := tool.NewGlobInvocation(invocationID, call)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}
