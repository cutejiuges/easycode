//go:build darwin || linux

package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/tool"
	"golang.org/x/sys/unix"
)

func TestWritePrepareFailsClosedWithoutMutation(t *testing.T) {
	for _, kind := range []string{"directory", "fifo", "unreadable", "unwritable", "special-mode", "binary", "invalid-utf8", "symlink", "too-large", "too-deep", "empty-existing"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "a")
			var err error
			switch kind {
			case "directory":
				err = os.Mkdir(path, 0o700)
			case "fifo":
				err = unix.Mkfifo(path, 0o600)
			case "symlink":
				err = os.Symlink(t.TempDir(), path)
			case "unreadable":
				err = os.WriteFile(path, []byte("base"), 0o200)
			case "unwritable":
				err = os.WriteFile(path, []byte("base"), 0o400)
			case "special-mode":
				err = os.WriteFile(path, []byte("base"), 0o600)
				if err == nil {
					err = unix.Chmod(path, 0o4600)
				}
			case "binary":
				err = os.WriteFile(path, []byte{0}, 0o600)
			case "invalid-utf8":
				err = os.WriteFile(path, []byte{0xff}, 0o600)
			case "too-large":
				err = os.WriteFile(path, []byte(strings.Repeat("x", maxReadFileBytes+1)), 0o600)
			case "empty-existing":
				err = os.WriteFile(path, nil, 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			_, p, _, _ := writeComponents(t, root)
			relative := "a"
			if kind == "too-deep" {
				relative = strings.Repeat("d/", 17) + "a"
			}
			if _, err := p.Prepare(context.Background(), writeCall(t, relative, "new"), tool.UnavailableReadObservation()); err == nil {
				t.Fatal("危险或无证据目标被接受")
			}
			assertNoStaging(t, root)
			if kind == "too-deep" {
				entries, err := os.ReadDir(root)
				if err != nil || len(entries) != 0 {
					t.Fatal("prepare 创建了目录")
				}
			}
		})
	}
}

func TestWriteCandidateDeterminismAndPreviewBound(t *testing.T) {
	root := t.TempDir()
	_, p, _, _ := writeComponents(t, root)
	v := writeCall(t, strings.Repeat("d/", 16)+"a", "\ufeff中文\r\n\x1b[2J\u202e尾行")
	one, err := p.Prepare(context.Background(), v, tool.UnavailableReadObservation())
	if err != nil {
		t.Fatal(err)
	}
	two, err := p.Prepare(context.Background(), v, tool.UnavailableReadObservation())
	if err != nil {
		t.Fatal(err)
	}
	if one.Fingerprint() != two.Fingerprint() || one.Preview() != two.Preview() || len(one.Directories()) != 16 {
		t.Fatal("候选不确定")
	}
	if string(one.FinalBytes()) != v.Input().Content() || strings.ContainsAny(one.Preview(), "\x1b\r\u202e\ufeff") {
		t.Fatal("显示转义改变 bytes 或保留终端控制")
	}
	for _, content := range []string{strings.Repeat("x", tool.MaxApprovalDiffBytes), strings.Repeat("\x1b", tool.MaxApprovalDiffBytes/2)} {
		_, err := p.Prepare(context.Background(), writeCall(t, "a", content), tool.UnavailableReadObservation())
		var coded *WritePreparationError
		if !errors.As(err, &coded) || coded.Code() != "write_preview_too_large" {
			t.Fatal("没有拒绝超限完整展示", err)
		}
	}
	assertNoStaging(t, root)
}

func TestWritePreviewGolden(t *testing.T) {
	input, _ := tool.NewWriteInput("a", "\ufeff中文\r\n\x1b\u202e\t尾行")
	actual, err := buildWritePreview(input, tool.WriteCreate, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	expected := "--- a/a\n+++ b/a\n@@ -0,0 +1,2 @@\n+\\u{FEFF}中文\\r\n+\\u{001B}\\u{202E}\\t尾行\n\\\\ No newline at end of file\n"
	if actual != expected {
		t.Fatalf("预览 golden 不同\n得到 %q\n期望 %q", actual, expected)
	}
	input, _ = tool.NewWriteInput("a", "x\n")
	actual, err = buildWritePreview(input, tool.WriteOverwrite, []byte("x\r\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	expected = "--- a/a\n+++ b/a\n@@ -1,1 +1,1 @@\n-x\\r\n+x\n"
	if actual != expected {
		t.Fatalf("CRLF diff 不同: %q", actual)
	}
}

func TestWriteCleanupPreservesUnknownEntriesAndClosesHandles(t *testing.T) {
	for _, change := range []string{"unknown-child", "replaced-identity", "rmdir-failure", "close-failure"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			_, p, e, _ := writeComponents(t, root)
			c, err := p.Prepare(context.Background(), writeCall(t, "new/sub/a", "data"), tool.UnavailableReadObservation())
			if err != nil {
				t.Fatal(err)
			}
			var closed []*os.File
			unknown := ""
			e.hooks.close = func(file *os.File) error {
				closed = append(closed, file)
				if change == "close-failure" {
					return unix.EIO
				}
				return nil
			}
			e.hooks.beforePublish = func() {
				entries, err := os.ReadDir(root)
				if err != nil || len(entries) != 1 {
					t.Fatal("暂存根不唯一", err)
				}
				stage := filepath.Join(root, entries[0].Name())
				switch change {
				case "unknown-child":
					unknown = filepath.Join(stage, "sub", "unknown")
					err = os.WriteFile(unknown, []byte("external"), 0o600)
				case "replaced-identity":
					leaf := filepath.Join(stage, "sub", "a")
					err = os.Rename(leaf, leaf+".owned")
					if err == nil {
						err = os.WriteFile(leaf, []byte("external"), 0o600)
					}
					unknown = leaf
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if change == "rmdir-failure" {
				e.hooks.fileSync = func(*os.File) error { return unix.EIO }
				e.hooks.unlink = func(fd int, name string, directory bool) error {
					if directory {
						return unix.EIO
					}
					return unix.Unlinkat(fd, name, 0)
				}
			}
			result := e.Execute(context.Background(), c)
			if result.Status() != tool.ResultOutcomeUncertain {
				t.Fatalf("清理失败未报告 uncertain %s/%s", result.Status(), result.Code())
			}
			if unknown != "" {
				data, err := os.ReadFile(unknown)
				if err != nil || string(data) != "external" {
					t.Fatal("删除了未知对象", err)
				}
			}
			if len(closed) != 3 {
				t.Fatal("未关闭所有暂存 fd", len(closed))
			}
			for _, file := range closed {
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Fatal("fd 未实际关闭", err)
				}
			}
		})
	}
}
