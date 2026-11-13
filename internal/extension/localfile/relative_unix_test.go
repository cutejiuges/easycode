//go:build darwin || linux

package localfile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRelativeReadUsesTheCheckedDirectoryAndRejectsReplacement(t *testing.T) {
	ctx := context.Background()
	root, other := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "refs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "refs", "guide.md"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "guide.md"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	directory, err := Open(ctx, root, true, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	opened, err := OpenRelative(ctx, directory, "refs/guide.md", false, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if err := os.Rename(filepath.Join(root, "refs"), filepath.Join(root, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(root, "refs")); err != nil {
		t.Fatal(err)
	}
	b, err := ReadOpened(ctx, opened, 64, Options{})
	if err != nil || string(b.Bytes) != "original" || b.ReadBytes != len("original") {
		t.Fatal("opened file switched", err)
	}
	if _, err := OpenRelative(ctx, directory, "refs/guide.md", false, Options{}); err == nil {
		t.Fatal("followed replaced directory")
	}
	for _, name := range []string{"../secret", "/absolute", "a//b", "a\\b", "a:bad", ".", ""} {
		if _, err := OpenRelative(ctx, directory, name, false, Options{}); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
}

func TestReadOpenedAccountsRejectedBytesWithoutDisclosingContent(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "document")
			if err := os.WriteFile(name, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			file, err := Open(context.Background(), name, false, Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			expected := error(Changed)
			if cancelled {
				expected = context.Canceled
			}
			content, err := ReadOpened(ctx, file, 64, Options{Consistent: true, AfterRead: func() {
				if cancelled {
					cancel()
				} else if err := os.WriteFile(name, []byte("changed contents"), 0600); err != nil {
					t.Fatal(err)
				}
			}})
			if err != expected || content.ReadBytes != len("original") || len(content.Bytes) != 0 {
				t.Fatal("rejected content lost accounting or leaked bytes", content.ReadBytes, err)
			}
		})
	}
}

func TestRelativeReplacementAtEachComponent(t *testing.T) {
	for _, stage := range []string{"refs", "refs/guide.md"} {
		t.Run(stage, func(t *testing.T) {
			root, target := t.TempDir(), t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "refs"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "refs", "guide.md"), []byte("safe"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, "guide.md"), []byte("secret"), 0600); err != nil {
				t.Fatal(err)
			}
			dir, err := Open(context.Background(), root, true, Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer dir.Close()
			_, err = OpenRelative(context.Background(), dir, "refs/guide.md", false, Options{BeforeOpen: func(p string) {
				if p != stage {
					return
				}
				at := filepath.Join(root, filepath.FromSlash(stage))
				if err := os.Rename(at, at+"-old"); err != nil {
					t.Fatal(err)
				}
				destination := target
				if stage == "refs/guide.md" {
					destination = filepath.Join(target, "guide.md")
				}
				if err := os.Symlink(destination, at); err != nil {
					t.Fatal(err)
				}
			}})
			if err == nil {
				t.Fatal("replacement escaped checked root")
			}
		})
	}
}
