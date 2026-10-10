//go:build darwin || linux

package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/tool"
	"golang.org/x/sys/unix"
)

func TestGlobDirectoryReplacementStaysBoundToOpenedHandle(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeSearchFile(t, root, "dir/safe.go", "safe")
	writeSearchFile(t, outside, "secret.go", "outside-secret")
	executor, workspace := openTestGlobExecutor(t, root)
	workspace.hooks.afterOpenComponent = func(component string) {
		if component != "dir" {
			return
		}
		workspace.hooks.afterOpenComponent = nil
		if err := os.Rename(filepath.Join(root, "dir"), filepath.Join(root, "opened-dir")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "dir")); err != nil {
			t.Fatal(err)
		}
	}
	result := executor.Execute(context.Background(), globInvocationFor(t, "dir/**/*.go", "", 100))
	if result.Status() != tool.ResultSuccess || result.Preview().Text() != "dir/safe.go\n" {
		t.Fatalf("安全目录 handle 未继续遍历: %s %q", result.Status(), result.Preview().Text())
	}
	if strings.Contains(result.Preview().Text(), "secret") || strings.Contains(result.Preview().Text(), outside) {
		t.Fatal("Glob 跟随了替换后的外部 symlink")
	}
}

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
	result := executor.Execute(context.Background(), grepInvocationFor(t, "needle", "", "*.txt", tool.GrepOutputContent, false, 0, 0, 100))
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

func TestSearchSkipsPermissionDisappearanceAndSpecialFilesWithoutLeakingCause(t *testing.T) {
	root := t.TempDir()
	writeSearchFile(t, root, "ok.txt", "needle")
	writeSearchFile(t, root, "denied.txt", "private needle")
	if err := os.Chmod(filepath.Join(root, "denied.txt"), 0); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := openTestGrepExecutor(t, root)
	executor.workspace.hooks.beforeOpenComponent = func(component string) {
		if component == "gone.txt" {
			_ = os.Remove(filepath.Join(root, "gone.txt"))
		}
	}
	writeSearchFile(t, root, "gone.txt", "needle")
	result := executor.Execute(context.Background(), grepInvocationFor(t, "needle", "", "", tool.GrepOutputContent, false, 0, 0, 100))
	skipped := result.GrepMetadata().Skipped()
	if skipped.Unreadable() == 0 || skipped.Unsupported() == 0 || skipped.Disappeared() == 0 {
		t.Fatalf("故障跳过统计不完整: %#v", skipped)
	}
	combined := result.Preview().Text() + result.Code()
	if strings.Contains(combined, root) || strings.Contains(combined, "private needle") {
		t.Fatalf("结果泄漏绝对路径或未读正文: %q", combined)
	}
}
