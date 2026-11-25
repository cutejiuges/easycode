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

func patchInvocationForTest(t *testing.T, id, path, oldText, newText string) tool.PatchInvocation {
	t.Helper()
	replacement, err := tool.NewPatchReplacement(oldText, newText, false)
	if err != nil {
		t.Fatal(err)
	}
	input, err := tool.NewPatchInput(path, []tool.PatchReplacement{replacement})
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := tool.ParseProviderCallID("call-patch")
	call, err := tool.NewPatchReadyCall(callID, tool.WireStructured, input)
	if err != nil {
		t.Fatal(err)
	}
	invocationID, _ := tool.ParseInvocationID(id)
	invocation, err := tool.NewPatchInvocation(invocationID, call)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}

func preparePatchForTest(t *testing.T, workspace *Workspace, invocation tool.PatchInvocation) tool.PatchCandidate {
	t.Helper()
	preparer, err := NewPatchPreparer(workspace)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := preparer.Prepare(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func TestPatchPrepareTOCTOUDoesNotFollowReplacementSymlink(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeBuiltinFixture(t, filepath.Join(root, "dir", "file"), []byte("old"), 0o644)
	writeBuiltinFixture(t, filepath.Join(outside, "file"), []byte("secret"), 0o644)
	workspace := openFixtureWorkspace(t, root)
	defer func() { _ = workspace.Close() }()
	workspace.hooks.afterOpenComponent = func(relative string) {
		if relative != "dir" {
			return
		}
		if err := os.Rename(filepath.Join(root, "dir"), filepath.Join(root, "original")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "dir")); err != nil {
			t.Fatal(err)
		}
	}
	candidate := preparePatchForTest(t, workspace, patchInvocationForTest(t, "018f1d8a-7b5c-7def-8123-456789abcdef", "dir/file", "old", "new"))
	if string(candidate.FinalBytes()) != "new" {
		t.Fatalf("candidate = %q", candidate.FinalBytes())
	}
	content, _ := os.ReadFile(filepath.Join(outside, "file"))
	if string(content) != "secret" {
		t.Fatal("读取或修改了workspace外目标")
	}
}

func TestPatchExecutorFailuresAndConflictAreFailClosed(t *testing.T) {
	for _, test := range []struct {
		name   string
		hook   func(*PatchExecutor, context.CancelFunc)
		status tool.ResultStatus
		code   string
		want   string
	}{
		{"write", func(e *PatchExecutor, _ context.CancelFunc) {
			e.hooks.write = func(*os.File, []byte) error { return errors.New("write fault") }
		}, tool.ResultError, "patch_write_failed", "old\n"},
		{"short-write", func(e *PatchExecutor, _ context.CancelFunc) {
			e.hooks.write = func(file *os.File, data []byte) error { _, err := file.Write(data[:1]); return err }
		}, tool.ResultError, "patch_write_failed", "old\n"},
		{"file-sync", func(e *PatchExecutor, _ context.CancelFunc) {
			e.hooks.fileSync = func(*os.File) error { return errors.New("sync fault") }
		}, tool.ResultError, "patch_write_failed", "old\n"},
		{"rename", func(e *PatchExecutor, _ context.CancelFunc) {
			e.hooks.rename = func(int, string, string) error { return errors.New("rename fault") }
		}, tool.ResultError, "patch_write_failed", "old\n"},
		{"conflict", func(e *PatchExecutor, _ context.CancelFunc) {
			e.hooks.beforeRename = func() { _ = os.WriteFile(filepath.Join(e.workspace.rootPath, "file"), []byte("changed\n"), 0o640) }
		}, tool.ResultConflict, "patch_conflict", "changed\n"},
		{"directory-sync", func(e *PatchExecutor, _ context.CancelFunc) {
			e.hooks.directorySync = func(*os.File) error { return errors.New("directory sync fault") }
		}, tool.ResultOutcomeUncertain, "patch_durability_uncertain", "new\n"},
		{"cancel-after-directory-sync", func(e *PatchExecutor, cancel context.CancelFunc) {
			e.hooks.directorySync = func(directory *os.File) error {
				if err := directory.Sync(); err != nil {
					return err
				}
				cancel()
				return nil
			}
		}, tool.ResultSuccess, "ok", "new\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "file")
			writeBuiltinFixture(t, path, []byte("old\n"), 0o640)
			workspace := openFixtureWorkspace(t, root)
			defer func() { _ = workspace.Close() }()
			candidate := preparePatchForTest(t, workspace, patchInvocationForTest(t, "018f1d8a-7b5c-7def-8123-456789abcdef", "file", "old", "new"))
			executor, _ := NewPatchExecutor(workspace)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			test.hook(executor, cancel)
			result := executor.Execute(ctx, candidate)
			if result.Status() != test.status || result.Code() != test.code {
				t.Fatalf("result status/code = %s/%s", result.Status(), result.Code())
			}
			content, _ := os.ReadFile(path)
			if string(content) != test.want {
				t.Fatalf("content = %q", content)
			}
			assertNoPatchTemporary(t, root)
		})
	}
}

func TestPatchExecutorOwnsCleanupBoundary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	writeBuiltinFixture(t, path, []byte("old\n"), 0o640)
	workspace := openFixtureWorkspace(t, root)
	defer func() { _ = workspace.Close() }()
	candidate := preparePatchForTest(t, workspace, patchInvocationForTest(t, "018f1d8a-7b5c-7def-8123-456789abcdef", "file", "old", "new"))
	executor, _ := NewPatchExecutor(workspace)
	cleaned := false
	executor.hooks.rename = func(int, string, string) error { return errors.New("rename fault") }
	executor.hooks.cleanup = func(parentFD int, name string) error {
		cleaned = true
		return unix.Unlinkat(parentFD, name, 0)
	}
	result := executor.Execute(context.Background(), candidate)
	if !cleaned || result.Status() != tool.ResultError {
		t.Fatalf("cleanup=%t result=%#v", cleaned, result)
	}
	assertNoPatchTemporary(t, root)
}

func TestPatchRejectsAndRetainsReplacedStagingEntry(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "file")
	writeBuiltinFixture(t, target, []byte("old\n"), 0o640)
	workspace := openFixtureWorkspace(t, root)
	defer func() { _ = workspace.Close() }()
	candidate := preparePatchForTest(t, workspace, patchInvocationForTest(t, "018f1d8a-7b5c-7def-8123-456789abcdef", "file", "old", "new"))
	executor, _ := NewPatchExecutor(workspace)
	var foreign string
	executor.hooks.beforeRename = func() {
		entries, err := filepath.Glob(filepath.Join(root, ".easycode-patch-*"))
		if err != nil || len(entries) != 1 {
			t.Fatalf("expected one staging entry: %v %v", entries, err)
		}
		foreign = entries[0]
		if err := os.Rename(foreign, filepath.Join(root, "owned")); err != nil {
			t.Fatal(err)
		}
		writeBuiltinFixture(t, foreign, []byte("external\n"), 0o640)
	}
	result := executor.Execute(context.Background(), candidate)
	if result.Status() != tool.ResultOutcomeUncertain || result.Code() != "patch_durability_uncertain" {
		t.Errorf("result = %s/%s", result.Status(), result.Code())
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "old\n" {
		t.Errorf("target was modified: %q %v", data, err)
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "external\n" {
		t.Errorf("foreign entry was removed or modified: %q %v", data, err)
	}
}

func assertNoPatchTemporary(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".easycode-patch-") {
			t.Fatalf("临时文件未清理: %s", entry.Name())
		}
	}
}
