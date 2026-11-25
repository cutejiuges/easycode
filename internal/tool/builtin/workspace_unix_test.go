//go:build darwin || linux

package builtin

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestWorkspaceOpenChildPinsDirectoryUntilOpenatReturns(t *testing.T) {
	root := t.TempDir()
	writeBuiltinFixture(t, filepath.Join(root, "file"), []byte("inside workspace"), 0o600)
	workspace := openFixtureWorkspace(t, root)
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
