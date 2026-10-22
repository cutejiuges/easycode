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

func TestPatchPrepareIsReadOnlyDeterministicAndHandleRelative(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "src", "main.go")
	if err := os.Mkdir(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspace.Close() }()
	first := patchInvocationForTest(t, "018f1d8a-7b5c-7def-8123-456789abcdef", "src/main.go", "old", "new")
	second := patchInvocationForTest(t, "018f1d8a-7b5c-7def-8abc-456789abcdef", "src/main.go", "old", "new")
	candidate := preparePatchForTest(t, workspace, first)
	again := preparePatchForTest(t, workspace, second)
	if candidate.Fingerprint() != again.Fingerprint() {
		t.Fatal("调用identity污染了candidate fingerprint")
	}
	content, _ := os.ReadFile(path)
	if string(content) != "old\n" {
		t.Fatalf("准备阶段修改了文件: %q", content)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 || entries[0].Name() != "main.go" {
		t.Fatalf("准备阶段创建了文件: %#v", entries)
	}
}

func TestPatchPrepareRejectsUnsafeTargets(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T, string) string
		code  string
	}{
		{"symlink", func(t *testing.T, root string) string {
			outside := filepath.Join(t.TempDir(), "outside")
			_ = os.WriteFile(outside, []byte("old"), 0o644)
			if err := os.Symlink(outside, filepath.Join(root, "target")); err != nil {
				t.Fatal(err)
			}
			return "target"
		}, "not_regular_file"},
		{"directory", func(t *testing.T, root string) string {
			if err := os.Mkdir(filepath.Join(root, "target"), 0o755); err != nil {
				t.Fatal(err)
			}
			return "target"
		}, "not_regular_file"},
		{"fifo", func(t *testing.T, root string) string {
			if err := unix.Mkfifo(filepath.Join(root, "target"), 0o600); err != nil {
				t.Fatal(err)
			}
			return "target"
		}, "not_regular_file"},
		{"readonly", func(t *testing.T, root string) string {
			if err := os.WriteFile(filepath.Join(root, "target"), []byte("old"), 0o400); err != nil {
				t.Fatal(err)
			}
			return "target"
		}, "permission_denied"},
		{"too-large", func(t *testing.T, root string) string {
			file, err := os.Create(filepath.Join(root, "target"))
			if err != nil {
				t.Fatal(err)
			}
			if err = file.Truncate(maxReadFileBytes + 1); err != nil {
				t.Fatal(err)
			}
			_ = file.Close()
			return "target"
		}, "file_too_large"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			relative := test.setup(t, root)
			workspace, err := OpenWorkspace(root)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = workspace.Close() }()
			preparer, _ := NewPatchPreparer(workspace)
			_, err = preparer.Prepare(context.Background(), patchInvocationForTest(t, "018f1d8a-7b5c-7def-8123-456789abcdef", relative, "old", "new"))
			if patchPreparationCode(err) != test.code {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if _, err := tool.NewPatchInput("../outside", nil); err == nil {
		t.Fatal("路径穿越未被typed input拒绝")
	}
}

func TestPatchPrepareTOCTOUDoesNotFollowReplacementSymlink(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dir", "file"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "file"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
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

func TestPatchExecutorAtomicSuccessPreservesModeAndCleansTemporary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, []byte("old\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspace.Close() }()
	candidate := preparePatchForTest(t, workspace, patchInvocationForTest(t, "018f1d8a-7b5c-7def-8123-456789abcdef", "file", "old", "new"))
	executor, _ := NewPatchExecutor(workspace)
	result := executor.Execute(context.Background(), candidate)
	if result.Status() != tool.ResultSuccess {
		t.Fatalf("result = %#v", result)
	}
	content, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if string(content) != "new\n" || info.Mode().Perm() != 0o640 {
		t.Fatalf("content %q mode %o", content, info.Mode().Perm())
	}
	assertNoPatchTemporary(t, root)
}

func TestPatchExecutorFailuresAndConflictAreFailClosed(t *testing.T) {
	for _, test := range []struct {
		name   string
		hook   func(*PatchExecutor)
		status tool.ResultStatus
		code   string
		want   string
	}{
		{"write", func(e *PatchExecutor) {
			e.hooks.write = func(*os.File, []byte) error { return errors.New("write fault") }
		}, tool.ResultError, "patch_write_failed", "old\n"},
		{"short-write", func(e *PatchExecutor) {
			e.hooks.write = func(file *os.File, data []byte) error { _, err := file.Write(data[:1]); return err }
		}, tool.ResultError, "patch_write_failed", "old\n"},
		{"file-sync", func(e *PatchExecutor) { e.hooks.fileSync = func(*os.File) error { return errors.New("sync fault") } }, tool.ResultError, "patch_write_failed", "old\n"},
		{"rename", func(e *PatchExecutor) {
			e.hooks.rename = func(int, string, string) error { return errors.New("rename fault") }
		}, tool.ResultError, "patch_write_failed", "old\n"},
		{"conflict", func(e *PatchExecutor) {
			e.hooks.beforeRename = func() { _ = os.WriteFile(filepath.Join(e.workspace.rootPath, "file"), []byte("changed\n"), 0o640) }
		}, tool.ResultConflict, "patch_conflict", "changed\n"},
		{"directory-sync", func(e *PatchExecutor) {
			e.hooks.directorySync = func(*os.File) error { return errors.New("directory sync fault") }
		}, tool.ResultOutcomeUncertain, "patch_durability_uncertain", "new\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "file")
			if err := os.WriteFile(path, []byte("old\n"), 0o640); err != nil {
				t.Fatal(err)
			}
			workspace, err := OpenWorkspace(root)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = workspace.Close() }()
			candidate := preparePatchForTest(t, workspace, patchInvocationForTest(t, "018f1d8a-7b5c-7def-8123-456789abcdef", "file", "old", "new"))
			executor, _ := NewPatchExecutor(workspace)
			test.hook(executor)
			result := executor.Execute(context.Background(), candidate)
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
	if err := os.WriteFile(path, []byte("old\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
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
