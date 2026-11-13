//go:build darwin || linux

package skill

import (
	"context"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestDescriptorBoundSymlinkSwapAndSpecialFile(t *testing.T) {
	root := t.TempDir()
	p := writeSkill(t, root, "easycode", "demo", "---\ndescription: okay\n---\nbody")
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	fired := false
	hook := func(path string) {
		if path != systemSkillPath(p) || fired {
			return
		}
		fired = true
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readFile(context.Background(), p, hook); err == nil || !fired {
		t.Fatal("symlink swap accepted")
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(p, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readFile(context.Background(), p, nil); err == nil {
		t.Fatal("FIFO accepted")
	}
}

func TestUnsafeProjectBoundaryPreservesUserSource(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, home, "claude", "user-skill", "---\ndescription: valid user\n---\nbody")
	local := NewLocal()
	if err := local.Load(context.Background(), root, home, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(local.Catalog().Entries()) != 1 || local.Entries()[0].Code != "skill_root_unavailable" {
		t.Fatal("unsafe project disabled independent user source")
	}
}
