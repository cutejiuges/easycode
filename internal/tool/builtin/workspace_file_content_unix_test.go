//go:build darwin || linux

package builtin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/domain"
	"golang.org/x/sys/unix"
)

func TestWorkspaceFileSnapshotPreservesBytesAndUsesBoundFD(t *testing.T) {
	root := t.TempDir()
	source := "\ufeffone\r\ntwo\r\nlast"
	if err := os.WriteFile(filepath.Join(root, "file"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	content, count, err := workspace.ReadWorkspaceFile(context.Background(), "file", domain.MaxFileSourceBytes)
	if err != nil || content != source || count != len(source) {
		t.Fatalf("snapshot: %q %d %v", content, count, err)
	}
	workspace.hooks.afterOpenComponent = func(relative string) {
		if relative != "file" {
			return
		}
		if err := os.Rename(filepath.Join(root, "file"), filepath.Join(root, "old")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "file"), []byte("replacement"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	content, _, err = workspace.ReadWorkspaceFile(context.Background(), "file", domain.MaxFileSourceBytes)
	if err != nil || content != source {
		t.Fatal("reopened replaced path", err)
	}
}

func TestWorkspaceFileSnapshotFailsClosedAtEveryEvidenceBoundary(t *testing.T) {
	for _, scenario := range []string{"before_fstat", "after_fstat", "changed", "size_mismatch", "mode", "utf8", "nul", "limit"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			source := []byte("one\ntwo\n")
			if scenario == "utf8" {
				source = []byte{0xff}
			}
			if scenario == "nul" {
				source = []byte{'a', 0}
			}
			if err := os.WriteFile(filepath.Join(root, "file"), source, 0o600); err != nil {
				t.Fatal(err)
			}
			workspace, err := OpenWorkspace(root)
			if err != nil {
				t.Fatal(err)
			}
			defer workspace.Close()
			calls := 0
			workspace.hooks.fileSnapshot = func(file *os.File) (readFileState, error) {
				calls++
				state, err := readFileSnapshot(file)
				if scenario == "before_fstat" && calls == 1 || scenario == "after_fstat" && calls == 2 {
					return readFileState{}, errors.New("injected failure")
				}
				if scenario == "size_mismatch" {
					state.size++
				}
				if scenario == "changed" && calls == 2 {
					state.modified[0]++
				}
				if scenario == "mode" {
					state.mode = 0
				}
				return state, err
			}
			limit := domain.MaxFileSourceBytes
			if scenario == "limit" {
				limit = 1
			}
			content, count, err := workspace.ReadWorkspaceFile(context.Background(), "file", limit)
			if err == nil || content != "" {
				t.Fatal("unverified content delivered")
			}
			if count > limit {
				t.Fatal("read beyond source budget")
			}
			if scenario == "after_fstat" || scenario == "changed" {
				var failure *domain.FileFailure
				if !errors.As(err, &failure) || failure.Kind() != domain.FileChanged || count != len(source) {
					t.Fatalf("changed state not rejected: %d %v", count, err)
				}
			}
		})
	}
}

func TestWorkspaceFileSnapshotRejectsUnsafeTypesAndTraversal(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("outside-private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "dirlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	for _, path := range []string{"link", "dirlink/secret", "dir", "fifo", "../secret", filepath.Join(outside, "secret"), "./link"} {
		content, count, err := workspace.ReadWorkspaceFile(context.Background(), path, domain.MaxFileSourceBytes)
		if err == nil || content != "" || count != 0 || strings.Contains(err.Error(), outside) || strings.Contains(err.Error(), "outside-private") {
			t.Fatalf("unsafe read %q: %d %v", path, count, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, count, err := workspace.ReadWorkspaceFile(ctx, "anything", domain.MaxFileSourceBytes); !errors.Is(err, context.Canceled) || count != 0 {
		t.Fatal("cancel before open", err)
	}
}

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
