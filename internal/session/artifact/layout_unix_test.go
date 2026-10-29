//go:build darwin || linux

package artifact

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func layoutDirectories(t *testing.T) (string, string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace, storage := filepath.Join(root, "project"), filepath.Join(root, "project-data")
	for _, p := range []string{workspace, storage} {
		if err = os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return root, workspace, storage
}

func TestLayoutUsesDirectoryAncestry(t *testing.T) {
	root, w, s := layoutDirectories(t)
	for _, tt := range []struct {
		name, storage, workspace string
		reads                    []string
		ok                       bool
	}{
		{"similar-prefix", s, w, []string{w}, true}, {"home", s, root, nil, false}, {"same", s, s, nil, false}, {"workspace-inside-storage", root, w, nil, false}, {"readable-system-root", s, w, []string{root}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l, err := OpenLayout(context.Background(), tt.storage, tt.workspace, tt.reads, []string{tt.workspace})
			if (err == nil) != tt.ok {
				t.Fatalf("layout: %v", err)
			}
			if l != nil {
				if err = l.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestLayoutRejectsReplacementAndPrivateSymlinks(t *testing.T) {
	root, w, s := layoutDirectories(t)
	l, err := OpenLayout(context.Background(), s, w, []string{w}, []string{w})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err = os.Rename(s, s+"-old"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(s, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = l.Check(context.Background()); err == nil {
		t.Fatal("replacement accepted")
	}
	alias := filepath.Join(root, "alias")
	if err = os.Symlink(s, alias); err != nil {
		t.Fatal(err)
	}
	if other, err := OpenLayout(context.Background(), alias, w, nil, nil); err == nil {
		_ = other.Close()
		t.Fatal("storage symlink accepted")
	}
	if other, err := OpenLayout(context.Background(), s, w, []string{alias}, nil); err == nil {
		_ = other.Close()
		t.Fatal("read-root alias bypassed ancestry")
	}
	if err = os.Chmod(s, 0o755); err != nil {
		t.Fatal(err)
	}
	if other, err := OpenLayout(context.Background(), s, w, nil, nil); err == nil {
		_ = other.Close()
		t.Fatal("public storage accepted")
	}
}
