//go:build darwin || linux

package artifact

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"easycode/internal/tool"
	"golang.org/x/sys/unix"
)

func TestArtifactFileTypeAndPermissionsAreCheckedOnOpen(t *testing.T) {
	for _, mode := range []string{"permissions", "directory", "fifo"} {
		t.Run(mode, func(t *testing.T) {
			s, root := testStore(t)
			c := startCapture(t, s)
			_, _ = c.WriteStdout([]byte("abc"))
			saving, _ := c.Finish(true, true, true)
			d, _ := saving.Descriptor()
			_ = c.Close()
			path := filepath.Join(root, string(s.threadID), string(c.invocation), string(d.ID()))
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatal("artifact permissions not private")
			}
			if mode == "permissions" {
				err = os.Chmod(path, 0o644)
			} else {
				if err = os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if mode == "directory" {
					err = os.Mkdir(path, 0o700)
				} else {
					err = unix.Mkfifo(path, 0o600)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			input, _ := tool.NewReadArtifactInput(d.ID(), 0, 3)
			if page, err := s.Read(context.Background(), d, input); err == nil || page.Text() != "" {
				t.Fatal("unsafe file returned bytes")
			}
		})
	}
}
