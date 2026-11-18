//go:build darwin || linux

package artifact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"easycode/internal/tool"
)

func TestCleanupPreservesCommittedReferencesAndRejectsActiveCapture(t *testing.T) {
	s, root := testStore(t)
	c := startCapture(t, s)
	_, _ = c.WriteStdout([]byte("retained"))
	out, _ := c.Finish(true, true, true)
	d, _ := out.Descriptor()
	path := filepath.Join(root, string(s.threadID), string(c.invocation), string(d.ID()))
	if err := s.Clean(context.Background(), nil); err == nil {
		t.Fatal("active capture was cleaned")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "retained" {
		t.Fatal("active object changed")
	}
	_ = c.Close()
	if err := os.Chtimes(path, time.Unix(0, 0), time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	otherPath := filepath.Join(root, "018f1d8a-7b5c-7def-8123-456789abcdee", string(c.invocation), string(d.ID()))
	if err := os.MkdirAll(filepath.Dir(otherPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, []byte("other thread"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(otherPath), "reservation"), []byte(commandReservation().wire), 0o600); err != nil {
		t.Fatal(err)
	}
	orphan := startCapture(t, s)
	_, _ = orphan.WriteStdout([]byte("orphan"))
	orphanPath := filepath.Join(root, string(s.threadID), string(orphan.invocation))
	_ = orphan.Close()
	if err := s.Clean(context.Background(), []tool.ArtifactDescriptor{d}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphanPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan retained: %v", err)
	}
	input, _ := tool.NewReadArtifactInput(d.ID(), 0, 100)
	page, err := s.Read(context.Background(), d, input)
	if err != nil || page.Text() != "retained" {
		t.Fatalf("committed object lost: %v", err)
	}
	if b, err := os.ReadFile(otherPath); err != nil || string(b) != "other thread" {
		t.Fatal("cleanup changed another thread")
	}
}
