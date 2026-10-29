//go:build darwin || linux

package artifact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"easycode/internal/tool"
	"golang.org/x/sys/unix"
)

func TestQuotaDoesNotDoubleCountPublicationAndRejectsCorruptReservation(t *testing.T) {
	s, root := testStore(t)
	s.threadBudget = tool.ArtifactInvocationBytes
	c := startCapture(t, s)
	_, _ = c.WriteStdout([]byte("abc"))
	_, _ = c.WriteStderr([]byte("def"))
	out, stderr := c.Finish(true, true, true)
	if out.Status() != tool.ArtifactComplete || stderr.Status() != tool.ArtifactComplete {
		t.Fatal("publication failed")
	}
	total, thread, err := s.usage(context.Background())
	if err != nil || total != tool.ArtifactInvocationBytes || thread != total {
		t.Fatalf("publication double charged: %d %d %v", total, thread, err)
	}
	id, _ := tool.GenerateInvocationID()
	if _, err = s.Start(context.Background(), id); !errors.Is(err, tool.ArtifactQuotaExceeded) {
		t.Fatal("thread reservation limit bypassed")
	}
	_ = c.Close()
	if total, _, err = s.usage(context.Background()); err != nil || total != 6 {
		t.Fatal("closed reservation did not retain actual bytes")
	}
	reservation := filepath.Join(root, string(s.threadID), string(c.invocation), "reservation")
	if err = os.WriteFile(reservation, []byte("0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Start(context.Background(), id); err == nil {
		t.Fatal("corrupt reservation released quota")
	}
	if b, err := os.ReadFile(reservation); err != nil || string(b) != "0\n" {
		t.Fatal("accounting silently repaired unknown reservation")
	}
}

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
