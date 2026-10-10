//go:build darwin || linux

package builtin

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
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
	names, err := directory.readEntryNames()
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
