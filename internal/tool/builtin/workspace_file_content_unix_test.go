//go:build darwin || linux

package builtin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"easycode/internal/domain"
)

func TestWorkspaceFileSnapshotParentReplacementCannotEscapeOpenedDirectory(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(fmt.Sprint(after), func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "dir"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "dir", "file"), []byte("inside"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outside, "file"), []byte("outside-secret"), 0600); err != nil {
				t.Fatal(err)
			}
			w, err := OpenWorkspace(root)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			replace := func(path string) {
				if path != "dir" {
					return
				}
				if err := os.Rename(filepath.Join(root, "dir"), filepath.Join(root, "held")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(root, "dir")); err != nil {
					t.Fatal(err)
				}
			}
			if after {
				w.hooks.afterOpenComponent = replace
			} else {
				w.hooks.beforeOpenComponent = replace
			}
			text, count, err := w.ReadWorkspaceFile(context.Background(), "dir/file", domain.MaxFileSourceBytes)
			if after {
				if err != nil || text != "inside" || count != 6 {
					t.Fatal("opened parent was reopened", err)
				}
			} else if err == nil || text != "" || count != 0 {
				t.Fatal("replaced parent escaped no-follow", err)
			}
		})
	}
}

func TestWorkspaceFileSnapshotCancellationWaitsForReadStack(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	workspace.hooks.afterFileRead = func(*os.File) { close(entered); <-release }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		content, _, err := workspace.ReadWorkspaceFile(ctx, "file", domain.MaxFileSourceBytes)
		if content != "" {
			finished <- errors.New("cancelled body delivered")
			return
		}
		finished <- err
	}()
	<-entered
	cancel()
	select {
	case <-finished:
		t.Fatal("reported completion before read stack returned")
	default:
	}
	close(release)
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestWorkspaceFileSnapshotCancellationDuringFinalCheckRemainsCancellation(t *testing.T) {
	for _, checkFails := range []bool{false, true} {
		t.Run(fmt.Sprint(checkFails), func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "file"), []byte("content"), 0o600); err != nil {
				t.Fatal(err)
			}
			workspace, err := OpenWorkspace(root)
			if err != nil {
				t.Fatal(err)
			}
			defer workspace.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			checks := 0
			workspace.hooks.fileSnapshot = func(file *os.File) (readFileState, error) {
				state, err := readFileSnapshot(file)
				checks++
				if checks == 2 {
					cancel()
					if checkFails {
						return readFileState{}, errors.New("injected final check failure")
					}
				}
				return state, err
			}
			content, count, err := workspace.ReadWorkspaceFile(ctx, "file", domain.MaxFileSourceBytes)
			if !errors.Is(err, context.Canceled) || content != "" || count != len("content") || checks != 2 {
				t.Fatalf("final check obscured cancellation: content=%q count=%d checks=%d err=%v", content, count, checks, err)
			}
		})
	}
}
