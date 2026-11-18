//go:build darwin || linux

package artifact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"easycode/internal/tool"
)

func TestReservationRejectsOldAndAlteredShapesWithoutRepair(t *testing.T) {
	for _, wire := range []string{"33554432\n", "{\"sources\":[\"search_result\"],\"bytes\":33554432}\n", "{\"sources\":[\"stdout\"],\"bytes\":16777216}\n", "{\"sources\":[\"search_result\"],\"bytes\":0}\n"} {
		s, root := testStore(t)
		c := startCapture(t, s)
		_ = c.Close()
		path := filepath.Join(root, string(s.threadID), string(c.invocation), "reservation")
		if err := os.WriteFile(path, []byte(wire), 0600); err != nil {
			t.Fatal(err)
		}
		id, _ := tool.GenerateInvocationID()
		if _, err := s.StartText(context.Background(), id, tool.CapabilityGrep); err == nil {
			t.Fatal("invalid reservation admitted")
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != wire {
			t.Fatal("invalid reservation mutated")
		}
	}
}

func TestTextCapturePublicationFailuresRemainUnavailable(t *testing.T) {
	for _, mode := range []string{"sync", "publish"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := testStore(t)
			id, _ := tool.GenerateInvocationID()
			c, err := s.StartText(context.Background(), id, tool.CapabilityGlob)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.Close() }()
			if err = c.WriteText("file.go\n"); err != nil {
				t.Fatal(err)
			}
			if mode == "sync" {
				c.storage.syncDirectory = func(*os.File) error { return errors.New("injected") }
			} else {
				c.storage.publish = func(*os.File, string, string) error { return errors.New("injected") }
			}
			saving := c.Finish(true, true)
			if _, ok := saving.Descriptor(); ok || saving.Status() != tool.ArtifactUnavailable {
				t.Fatal("failed publication exposed descriptor")
			}
			total, _, err := s.usage(context.Background())
			if err != nil || total != tool.ArtifactStreamBytes {
				t.Fatal("publication failure released live reservation")
			}
			_ = c.Close()
			total, _, err = s.usage(context.Background())
			if err != nil || total != int64(len("file.go\n")) {
				t.Fatal("failed capture accounting lost bytes")
			}
		})
	}
}

func TestSearchObjectsRejectReplacedLinksAndSameLengthCorruption(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "corrupt"} {
		t.Run(kind, func(t *testing.T) {
			store, root := testStore(t)
			id, _ := tool.GenerateInvocationID()
			capture, err := store.StartText(context.Background(), id, tool.CapabilityGrep)
			if err != nil {
				t.Fatal(err)
			}
			if err := capture.WriteText("original\n"); err != nil {
				t.Fatal(err)
			}
			saving := capture.Finish(true, true)
			descriptor, ok := saving.Descriptor()
			if !ok {
				t.Fatal("missing search descriptor")
			}
			if err := capture.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, string(store.threadID), string(id), string(descriptor.ID()))
			if kind == "corrupt" {
				if err := os.WriteFile(path, []byte("modified\n"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				target := filepath.Join(t.TempDir(), "target")
				if err := os.WriteFile(target, []byte("original\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink" {
					err = os.Symlink(target, path)
				} else {
					err = os.Link(target, path)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			input, _ := tool.NewReadArtifactInput(descriptor.ID(), 0, 100)
			_, err = store.Read(context.Background(), descriptor, input)
			want := tool.ArtifactReadUnavailable
			if kind == "corrupt" {
				want = tool.ArtifactReadCorrupt
			}
			if !errors.Is(err, want) {
				t.Fatalf("replaced search object: %v", err)
			}
		})
	}
}
