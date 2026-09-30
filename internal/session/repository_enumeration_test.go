//go:build darwin || linux

package session

import (
	"bytes"
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"easycode/internal/domain"
)

func TestRepositoryEnumerateJournalsIsDeterministic(t *testing.T) {
	repository, err := OpenOrCreateRepository(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()

	empty, err := repository.EnumerateJournals(context.Background())
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty enumeration = %#v, %v", empty, err)
	}
	ids := []domain.ThreadID{
		mustEnumerationThreadID(t, "2024-01-02T03:04:05Z", 1),
		mustEnumerationThreadID(t, "2025-12-31T23:59:59Z", 2),
		mustEnumerationThreadID(t, "2024-06-15T00:00:00Z", 3),
	}
	for _, threadID := range ids {
		lease, createErr := repository.Create(context.Background(), threadID)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if closeErr := lease.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	locations, err := repository.EnumerateJournals(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(locations) != len(ids) {
		t.Fatalf("location count = %d, want %d", len(locations), len(ids))
	}
	for index := 1; index < len(locations); index++ {
		if locations[index-1].RelativePath >= locations[index].RelativePath {
			t.Fatalf("locations are not sorted: %#v", locations)
		}
	}
}

func TestRepositoryEnumerateJournalsRejectsInvalidCandidateDate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	repository, err := OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	if err := os.MkdirAll(filepath.Join(root, "2024", "13"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.EnumerateJournals(context.Background()); err == nil {
		t.Fatal("EnumerateJournals() accepted invalid month")
	}
}

func TestRepositoryEnumerateJournalsRejectsCandidateSymlink(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	repository, err := OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	threadID, err := domain.GenerateThreadID()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := journalRelativePath(threadID)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Dir(filepath.Join(root, filepath.FromSlash(relative)))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(target, []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, filepath.FromSlash(relative))); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.EnumerateJournals(context.Background()); err == nil {
		t.Fatal("EnumerateJournals() unexpectedly followed a symlink")
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "outside-secret" {
		t.Fatalf("outside target changed: %q, %v", content, err)
	}
}

func TestRepositoryEnumerateJournalsRejectsUnsafeCandidateTypeAndPermissions(t *testing.T) {
	for _, test := range []struct {
		name   string
		create func(*testing.T, string)
	}{
		{name: "directory", create: func(t *testing.T, candidate string) {
			if err := os.Mkdir(candidate, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "permissions", create: func(t *testing.T, candidate string) {
			if err := os.WriteFile(candidate, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "sessions")
			repository, err := OpenOrCreateRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = repository.Close() }()
			threadID := mustEnumerationThreadID(t, "2024-03-04T00:00:00Z", 5)
			relative, err := journalRelativePath(threadID)
			if err != nil {
				t.Fatal(err)
			}
			candidate := filepath.Join(root, filepath.FromSlash(relative))
			if err := os.MkdirAll(filepath.Dir(candidate), 0o700); err != nil {
				t.Fatal(err)
			}
			test.create(t, candidate)
			if _, err := repository.EnumerateJournals(context.Background()); err == nil {
				t.Fatal("EnumerateJournals() accepted unsafe candidate")
			}
		})
	}
}

func TestRepositoryEnumerateJournalsRejectsJournalReplacement(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	repository, err := OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	threadID := mustEnumerationThreadID(t, "2024-03-04T00:00:00Z", 4)
	lease, err := repository.Create(context.Background(), threadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	journalPath, err := repository.JournalPath(threadID)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	replaced := false
	repository.root.hooks.beforeOpenJournal = func(string) {
		if replaced {
			return
		}
		replaced = true
		if err := os.Remove(journalPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, journalPath); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repository.EnumerateJournals(context.Background()); err == nil {
		t.Fatal("EnumerateJournals() followed replacement symlink")
	}
	content, err := os.ReadFile(outside)
	if err != nil || string(content) != "outside-secret" {
		t.Fatalf("outside file changed: %q, %v", content, err)
	}
}

func mustEnumerationThreadID(t *testing.T, timestamp string, marker byte) domain.ThreadID {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		t.Fatal(err)
	}
	raw := [16]byte{}
	milliseconds := parsed.UnixMilli()
	for index := 5; index >= 0; index-- {
		raw[index] = byte(milliseconds)
		milliseconds >>= 8
	}
	copy(raw[6:], bytes.Repeat([]byte{marker}, 10))
	raw[6] = raw[6]&0x0f | 0x70
	raw[8] = raw[8]&0x3f | 0x80
	compact := make([]byte, 32)
	hex.Encode(compact, raw[:])
	value := string(compact[:8]) + "-" + string(compact[8:12]) + "-" + string(compact[12:16]) + "-" + string(compact[16:20]) + "-" + string(compact[20:])
	threadID, err := domain.ParseThreadID(value)
	if err != nil {
		t.Fatal(err)
	}
	return threadID
}
