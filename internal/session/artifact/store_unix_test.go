//go:build darwin || linux

package artifact

import (
	"bytes"
	"context"
	"errors"
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

func TestCapturePublishesVerifiedRawOutput(t *testing.T) {
	s, root := testStore(t)
	c := startCapture(t, s)
	raw := []byte("first\n中\xff\nlast\n")
	if _, err := c.WriteStdout(raw); err != nil {
		t.Fatal(err)
	}
	out, stderr := c.Finish(true, true, true)
	d, ok := out.Descriptor()
	if !ok || !d.Complete() || stderr.Status() != tool.ArtifactNotNeeded {
		t.Fatalf("saving: %s %s", out.Status(), stderr.Status())
	}
	input, _ := tool.NewReadArtifactInput(d.ID(), 0, tool.ArtifactPageBytes)
	p, err := s.Read(context.Background(), d, input)
	if err != nil || p.Text() != "first\n中\\xFF\nlast\n" || !p.EOF() || !p.SourceComplete() {
		t.Fatalf("page: %+v %v", p, err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	total, thread, err := s.usage(context.Background())
	if err != nil || total != int64(len(raw)) || thread != total {
		t.Fatalf("usage: %d %d %v", total, thread, err)
	}
	path := filepath.Join(root, string(s.threadID), string(c.invocation), string(d.ID()))
	if err = os.WriteFile(path, bytes.Repeat([]byte{'x'}, len(raw)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Read(context.Background(), d, input); !errors.Is(err, tool.ArtifactReadCorrupt) {
		t.Fatalf("tampering: %v", err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Read(context.Background(), d, input); !errors.Is(err, tool.ArtifactReadUnavailable) {
		t.Fatalf("missing: %v", err)
	}
}

func TestCaptureBoundsContinuousPrefix(t *testing.T) {
	s, _ := testStore(t)
	c := startCapture(t, s)
	chunk := bytes.Repeat([]byte{'a'}, 64<<10)
	for i := 0; i < tool.ArtifactStreamBytes/len(chunk)+2; i++ {
		n, err := c.WriteStdout(chunk)
		if err != nil || n != len(chunk) {
			t.Fatalf("drain: %d %v", n, err)
		}
	}
	out, _ := c.Finish(true, true, true)
	d, ok := out.Descriptor()
	if !ok || out.Status() != tool.ArtifactPartial || out.Reason() != "stream_limit" || d.SavedBytes() != tool.ArtifactStreamBytes || d.ObservedBytes() != tool.ArtifactStreamBytes+2*int64(len(chunk)) {
		t.Fatalf("wrong prefix: %+v", d)
	}
	input, _ := tool.NewReadArtifactInput(d.ID(), uint64(d.SavedBytes())-3, 64)
	p, err := s.Read(context.Background(), d, input)
	if err != nil || p.Text() != "aaa" || !p.EOF() || p.SourceComplete() {
		t.Fatalf("partial end: %+v %v", p, err)
	}
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

func TestCaptureSmallOutputAndQuota(t *testing.T) {
	s, _ := testStore(t)
	s.rootBudget = tool.ArtifactInvocationBytes
	c := startCapture(t, s)
	id, _ := tool.GenerateInvocationID()
	if _, err := s.Start(context.Background(), id); !errors.Is(err, tool.ArtifactQuotaExceeded) {
		t.Fatalf("reservation exceeded: %v", err)
	}
	_, _ = c.WriteStdout([]byte("small"))
	out, _ := c.Finish(true, true, false)
	if out.Status() != tool.ArtifactNotNeeded {
		t.Fatal("small output stored")
	}
	_ = c.Close()
	if total, _, err := s.usage(context.Background()); err != nil || total != 0 {
		t.Fatalf("unused reservation retained: %d %v", total, err)
	}
	next, err := s.Start(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	_ = next.Close()
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
