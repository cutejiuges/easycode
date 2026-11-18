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

func TestGrepEnumerationToOpenReplacementDoesNotReadOutside(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeSearchFile(t, root, "victim.txt", "safe needle")
	secret := "outside-secret-needle"
	writeSearchFile(t, outside, "secret.txt", secret)
	executor := openTestGrepExecutor(t, root)
	executor.workspace.hooks.afterOpenComponent = func(component string) {
		if component != "victim.txt" {
			return
		}
		executor.workspace.hooks.afterOpenComponent = nil
		if err := os.Rename(filepath.Join(root, "victim.txt"), filepath.Join(root, "original.txt")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "victim.txt")); err != nil {
			t.Fatal(err)
		}
	}
	result := finalizeSearchForTest(t, executor.Execute(context.Background(), grepInvocationFor(t, "needle", "", "*.txt", tool.GrepOutputContent, false, 0, 0, 100), nil))
	if result.Status() != tool.ResultSuccess {
		t.Fatalf("Grep 失败: %s/%s", result.Status(), result.Code())
	}
	if strings.Contains(result.Preview().Text(), secret) || strings.Contains(result.Preview().Text(), outside) {
		t.Fatal("Grep 在枚举和 open 之间跟随了外部 symlink")
	}
	if result.GrepMetadata().Skipped().Disappeared() == 0 {
		t.Fatalf("替换条目未计入稳定跳过统计: %#v", result.GrepMetadata().Skipped())
	}
}
