package builtin

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSecureWalkerIsDeterministicIncludesHiddenAndSkipsVCS(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"z.go", "a.go", ".config/tool.yaml", "nested/b.go", ".git/config", ".jj/repo/store"} {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	first, err := walkWorkspace(context.Background(), workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(root, "a.go"), testLaterTime(), testLaterTime()); err != nil {
		t.Fatal(err)
	}
	second, err := walkWorkspace(context.Background(), workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".config/tool.yaml", "a.go", "nested/b.go", "z.go"}
	if got := filePaths(first.files); !reflect.DeepEqual(got, want) {
		t.Fatalf("walker paths = %#v", got)
	}
	if !reflect.DeepEqual(filePaths(first.files), filePaths(second.files)) {
		t.Fatal("mtime 改变了 walker 顺序")
	}
}

func TestSecureWalkerReturnsWorkspaceRelativePathsFromSubdirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "nested", "main.go"), []byte("package main"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	result, err := walkWorkspace(context.Background(), workspace, "src")
	if err != nil {
		t.Fatal(err)
	}
	if got := filePaths(result.files); !reflect.DeepEqual(got, []string{"src/nested/main.go"}) {
		t.Fatalf("subdirectory paths = %#v", got)
	}
}

func filePaths(files []workspaceFile) []string {
	paths := make([]string, len(files))
	for index := range files {
		paths[index] = files[index].relativePath
	}
	return paths
}

func testLaterTime() time.Time { return time.Unix(2_000_000_000, 0) }
