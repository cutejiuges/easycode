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

func TestReadRejectsSpecialFileAndUnreadableMode(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "denied"), []byte("secret"), 0o000); err != nil {
		t.Fatal(err)
	}
	executor, _ := openTestExecutor(t, root)
	if result := executor.Execute(context.Background(), invocationFor(t, "pipe", 1, 1)); result.Code() != "unsupported_file_type" {
		t.Fatalf("FIFO code = %q", result.Code())
	}
	if result := executor.Execute(context.Background(), invocationFor(t, "denied", 1, 1)); result.Code() != "permission_denied" {
		t.Fatalf("无可读 mode code = %q", result.Code())
	}
}

func TestReadTOCTOUStaysBoundToOpenedDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dir", "file"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := "outside-secret"
	if err := os.WriteFile(filepath.Join(outside, "file"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	executor, workspace := openTestExecutor(t, root)
	workspace.hooks.afterOpenComponent = func(component string) {
		if component != "dir" {
			return
		}
		workspace.hooks.afterOpenComponent = nil
		if err := os.Rename(filepath.Join(root, "dir"), filepath.Join(root, "safe-dir")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "dir")); err != nil {
			t.Fatal(err)
		}
	}
	result := executor.Execute(context.Background(), invocationFor(t, "dir/file", 1, 1))
	if result.Status() != tool.ResultSuccess || !strings.Contains(result.Preview().Text(), "safe") {
		t.Fatalf("已打开安全目录未继续读取: %s %q", result.Status(), result.Preview().Text())
	}
	if strings.Contains(result.Preview().Text(), secret) {
		t.Fatal("TOCTOU 读取了 workspace 外目标")
	}
}

func TestReadCancellationAfterAdmissionClosesFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte(strings.Repeat("x", 1<<20)), 0o600); err != nil {
		t.Fatal(err)
	}
	executor, workspace := openTestExecutor(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	workspace.hooks.afterOpenComponent = func(string) { cancel() }
	result := executor.Execute(ctx, invocationFor(t, "file", 1, 1))
	if result.Status() != tool.ResultCancelled {
		t.Fatalf("取消结果 = %s/%s", result.Status(), result.Code())
	}
	if err := os.Rename(filepath.Join(root, "file"), filepath.Join(root, "renamed")); err != nil {
		t.Fatalf("取消后文件资源未释放: %v", err)
	}
}
