//go:build darwin || linux

package artifact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"easycode/internal/tool"
)

func TestTextCapturePublicationAndUTF8Limit(t *testing.T) {
	for _, test := range []struct {
		name, text       string
		complete, needed bool
		status           tool.ArtifactSaveStatus
	}{
		{"complete", "路径:1:结果\n", true, true, tool.ArtifactComplete},
		{"not_needed", "small", true, false, tool.ArtifactNotNeeded},
		{"interrupted", "prefix", false, true, tool.ArtifactPartial},
		{"utf8_limit", strings.Repeat("a", tool.ArtifactStreamBytes-1) + "中尾", true, true, tool.ArtifactPartial},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, root := testStore(t)
			id, _ := tool.GenerateInvocationID()
			c, err := s.StartText(context.Background(), id, tool.CapabilityGrep)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.Close() }()
			if err = c.WriteText(test.text); err != nil {
				t.Fatal(err)
			}
			saving := c.Finish(test.complete, test.needed)
			if saving.Status() != test.status {
				t.Fatalf("saving: %s %s", saving.Status(), saving.Reason())
			}
			d, saved := saving.Descriptor()
			if saved {
				if d.Stream() != tool.ArtifactSearchResult || d.ObservedBytes() != int64(len(test.text)) {
					t.Fatal("wrong source accounting")
				}
				data, err := os.ReadFile(filepath.Join(root, string(s.threadID), string(id), string(d.ID())))
				if err != nil || !utf8.Valid(data) || !strings.HasPrefix(test.text, string(data)) {
					t.Fatalf("invalid saved prefix: %v", err)
				}
				if test.name == "utf8_limit" && (len(data) != tool.ArtifactStreamBytes-1 || saving.Reason() != "stream_limit") {
					t.Fatal("UTF-8 boundary lost")
				}
				input, _ := tool.NewReadArtifactInput(d.ID(), 0, tool.ArtifactPageBytes)
				if _, err = s.Read(context.Background(), d, input); err != nil {
					t.Fatal(err)
				}
			}
			if err = c.WriteText("late"); err == nil {
				t.Fatal("write after finish accepted")
			}
			if _, ok := c.Finish(true, true).Descriptor(); ok {
				t.Fatal("second publication succeeded")
			}
		})
	}
}

func TestTextCaptureQuotaAndSourceAdmission(t *testing.T) {
	s, _ := testStore(t)
	s.threadBudget = tool.ArtifactInvocationBytes + tool.ArtifactStreamBytes
	command := startCapture(t, s)
	id, _ := tool.GenerateInvocationID()
	if _, err := s.StartText(context.Background(), id, tool.CapabilityRead); !errors.Is(err, tool.ArtifactReadUnauthorized) {
		t.Fatal("non-search admitted")
	}
	c, err := s.StartText(context.Background(), id, tool.CapabilityGlob)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(); _ = command.Close() }()
	total, _, err := s.usage(context.Background())
	if err != nil || total != s.threadBudget {
		t.Fatalf("mixed reservations: %d %v", total, err)
	}
	next, _ := tool.GenerateInvocationID()
	if _, err = s.StartText(context.Background(), next, tool.CapabilityGrep); !errors.Is(err, tool.ArtifactQuotaExceeded) {
		t.Fatal("mixed quota exceeded")
	}
	if err = c.WriteText(string([]byte{255})); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	if err = c.WriteText("abc"); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	total, _, err = s.usage(context.Background())
	if err != nil || total != tool.ArtifactInvocationBytes+3 {
		t.Fatalf("released reservation: %d %v", total, err)
	}
}

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

func TestTextCaptureShortWriteKeepsValidPrefix(t *testing.T) {
	s, root := testStore(t)
	id, _ := tool.GenerateInvocationID()
	c, err := s.StartText(context.Background(), id, tool.CapabilityGrep)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err = c.WriteText("prefix\n"); err != nil {
		t.Fatal(err)
	}
	c.storage.stdout.file = failingCaptureFile{captureFile: c.storage.stdout.file, short: true}
	if err = c.WriteText("a中文"); err != nil {
		t.Fatal(err)
	}
	saving := c.Finish(true, true)
	d, ok := saving.Descriptor()
	if !ok || saving.Status() != tool.ArtifactPartial || saving.Reason() != "write_failed" {
		t.Fatal("partial text write not reported")
	}
	data, err := os.ReadFile(filepath.Join(root, string(s.threadID), string(id), string(d.ID())))
	if err != nil || !utf8.Valid(data) || string(data) != "prefix\na" {
		t.Fatalf("partial UTF-8 prefix: %q %v", data, err)
	}
	input, _ := tool.NewReadArtifactInput(d.ID(), 0, 100)
	if _, err = s.Read(context.Background(), d, input); err != nil {
		t.Fatal(err)
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
