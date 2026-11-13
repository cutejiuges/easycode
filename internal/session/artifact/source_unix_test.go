//go:build darwin || linux

package artifact

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceIsolationUsesActualDescriptorAncestry(t *testing.T) {
	root, workspace, storage := layoutDirectories(t)
	l, err := OpenLayout(context.Background(), storage, workspace, []string{workspace}, []string{workspace})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	inside := filepath.Join(storage, "plugin")
	similar := storage + "-plugin"
	for _, dir := range []string{inside, similar} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		dir   string
		valid bool
	}{{root, false}, {storage, false}, {inside, false}, {workspace, true}, {similar, true}} {
		f, err := openDirectoryPath(tt.dir, false)
		if err != nil {
			t.Fatal(err)
		}
		err = l.CheckSource(context.Background(), f)
		_ = f.Close()
		if (err == nil) != tt.valid {
			t.Fatalf("source %s: %v", tt.dir, err)
		}
	}
	f, err := openDirectoryPath(similar, false)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.Rename(similar, similar+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(storage, similar); err != nil {
		t.Fatal(err)
	}
	if err := l.CheckSource(context.Background(), f); err != nil {
		t.Fatal("checked FD changed to path replacement", err)
	}
	if err := os.Rename(storage, storage+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(storage, 0700); err != nil {
		t.Fatal(err)
	}
	if err := l.CheckSource(context.Background(), f); err == nil {
		t.Fatal("storage replacement accepted")
	}
}
