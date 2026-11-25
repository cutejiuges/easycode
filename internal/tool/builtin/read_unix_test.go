//go:build darwin || linux

package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/tool"
)

func TestReadCancellationAfterAdmissionClosesFile(t *testing.T) {
	root := t.TempDir()
	writeBuiltinFixture(t, filepath.Join(root, "file"), []byte(strings.Repeat("x", 1<<20)), 0o600)
	executor, workspace := openTestExecutor(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	workspace.hooks.afterOpenComponent = func(string) { cancel() }
	result := executor.Execute(ctx, invocationFor(t, "file", 1, 1)).Result()
	if result.Status() != tool.ResultCancelled {
		t.Fatalf("取消结果 = %s/%s", result.Status(), result.Code())
	}
	if err := os.Rename(filepath.Join(root, "file"), filepath.Join(root, "renamed")); err != nil {
		t.Fatalf("取消后文件资源未释放: %v", err)
	}
}
