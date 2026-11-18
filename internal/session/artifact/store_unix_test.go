//go:build darwin || linux

package artifact

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/tool"
	"golang.org/x/sys/unix"
)

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	_, w, s := layoutDirectories(t)
	l, err := OpenLayout(context.Background(), s, w, []string{w}, []string{w})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	store, err := Open(context.Background(), l, domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, filepath.Join(s, "artifacts")
}

func startCapture(t *testing.T, s *Store) *Capture {
	t.Helper()
	id, err := tool.GenerateInvocationID()
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Start(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

type failingCaptureFile struct {
	captureFile
	writeErr, syncErr error
	short             bool
}

func (f failingCaptureFile) Write(data []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	if f.short {
		return f.captureFile.Write(data[:len(data)/2])
	}
	return f.captureFile.Write(data)
}
func (f failingCaptureFile) Sync() error {
	if f.syncErr != nil {
		return f.syncErr
	}
	return f.captureFile.Sync()
}

func TestCaptureFailureDoesNotStopDraining(t *testing.T) {
	for _, mode := range []string{"short", "write", "enospc", "sync"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := testStore(t)
			c := startCapture(t, s)
			_, _ = c.WriteStdout([]byte("prefix"))
			f := failingCaptureFile{captureFile: c.stdout.file, short: mode == "short"}
			if mode == "write" {
				f.writeErr = io.ErrClosedPipe
			}
			if mode == "enospc" {
				f.writeErr = unix.ENOSPC
			}
			if mode == "sync" {
				f.syncErr = io.ErrClosedPipe
			}
			c.stdout.file = f
			for i := 0; i < 3; i++ {
				if n, err := c.WriteStdout([]byte("tail")); err != nil || n != 4 {
					t.Fatalf("pipe interrupted: %d %v", n, err)
				}
			}
			out, _ := c.Finish(true, true, true)
			if mode == "sync" {
				if out.Status() != tool.ArtifactUnavailable {
					t.Fatal("Sync failure published reference")
				}
			} else {
				if out.Status() != tool.ArtifactPartial || out.Reason() != "write_failed" {
					t.Fatal("write failure not reported")
				}
				d, _ := out.Descriptor()
				if d.ObservedBytes() != 18 {
					t.Fatal("lost drain count")
				}
			}
		})
	}
}

func TestCaptureRequiresEOFForCompleteStorage(t *testing.T) {
	for _, text := range []string{"", "prefix"} {
		s, _ := testStore(t)
		c := startCapture(t, s)
		_, _ = c.WriteStdout([]byte(text))
		out, stderr := c.Finish(false, true, true)
		if stderr.Status() != tool.ArtifactNotNeeded {
			t.Fatal("confirmed empty stream was not omitted")
		}
		if text == "" {
			if out.Status() != tool.ArtifactUnavailable {
				t.Fatal("unconfirmed empty stream claimed complete")
			}
			continue
		}
		d, ok := out.Descriptor()
		if !ok || out.Status() != tool.ArtifactPartial || d.Complete() || out.Reason() != "output_incomplete" {
			t.Fatal("unconfirmed prefix claimed complete")
		}
		input, _ := tool.NewReadArtifactInput(d.ID(), 0, 65536)
		page, err := s.Read(context.Background(), d, input)
		if err != nil || !page.EOF() || page.SourceComplete() || page.Text() != text {
			t.Fatal("stored EOF confused with source EOF")
		}
	}
}

func TestArtifactRejectsObjectSymlinksAndHardlinks(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			s, root := testStore(t)
			c := startCapture(t, s)
			_, _ = c.WriteStdout([]byte("abc"))
			out, _ := c.Finish(true, true, true)
			d, _ := out.Descriptor()
			_ = c.Close()
			path := filepath.Join(root, string(s.threadID), string(c.invocation), string(d.ID()))
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("abc"), 0o600); err != nil {
				t.Fatal(err)
			}
			if kind == "symlink" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Link(path, outside+"-alias"); err != nil {
					t.Fatal(err)
				}
			}
			input, _ := tool.NewReadArtifactInput(d.ID(), 0, 3)
			if p, err := s.Read(context.Background(), d, input); err == nil || p.Text() != "" {
				t.Fatal("unsafe object read")
			}
			body, err := os.ReadFile(outside)
			if err != nil || !strings.EqualFold(string(body), "abc") {
				t.Fatal("outside bytes changed")
			}
		})
	}
}
