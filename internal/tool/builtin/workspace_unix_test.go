//go:build darwin || linux

package builtin

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"
)

func TestWorkspaceHandleEnumeratesAndClassifiesChildren(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	directory, err := workspace.handle.openDirectory("", workspaceHooks{})
	if err != nil {
		t.Fatal(err)
	}
	names, err := directory.readEntryNames(workspaceNameBatch)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "dir" || names[1] != "file" {
		t.Fatalf("目录项错误: %#v", names)
	}
	childDirectory, err := directory.openChild("dir", workspaceHooks{})
	if err != nil || childDirectory.kind() != workspaceNodeDirectory || childDirectory.directory() == nil {
		t.Fatalf("目录 child 错误: kind=%v err=%v", childDirectory.kind(), err)
	}
	if err := childDirectory.close(); err != nil {
		t.Fatal(err)
	}
	childFile, err := directory.openChild("file", workspaceHooks{})
	if err != nil || childFile.kind() != workspaceNodeRegular || childFile.file() == nil || childFile.size() != 7 {
		t.Fatalf("文件 child 错误: kind=%v size=%d err=%v", childFile.kind(), childFile.size(), err)
	}
	if err := childFile.close(); err != nil {
		t.Fatal(err)
	}
	if err := directory.close(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceOpenChildPinsDirectoryUntilOpenatReturns(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("inside workspace"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	directory, err := workspace.handle.openDirectory("", workspaceHooks{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = directory.close() })
	closed := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var barrierErr error
	child, openErr := directory.openChild("file", workspaceHooks{beforeOpenComponent: func(string) {
		go func() { closed <- directory.interrupt() }()
		// 观察 os.File 的关闭门已经生效，再继续 openat；无需用 sleep 猜测 Close 的时序。
		for {
			_, err := directory.(*unixWorkspaceNode).handle.Stat()
			if errors.Is(err, os.ErrClosed) {
				break
			}
			if err != nil {
				barrierErr = err
				return
			}
			if ctx.Err() != nil {
				barrierErr = ctx.Err()
				return
			}
			runtime.Gosched()
		}
	}})
	if child != nil {
		t.Cleanup(func() { _ = child.close() })
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if barrierErr != nil || openErr != nil {
		t.Fatalf("accepted open lost its pinned directory: barrier=%v open=%v", barrierErr, openErr)
	}
	content, err := io.ReadAll(child.file())
	if err != nil || string(content) != "inside workspace" {
		t.Fatalf("opened another object: %q %v", content, err)
	}
	if unexpected, err := directory.openChild("file", workspaceHooks{}); err == nil || unexpected != nil {
		t.Fatal("closed directory accepted another open")
	}
}
