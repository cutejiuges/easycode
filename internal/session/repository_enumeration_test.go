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

func TestRepositoryEnumerateJournalsRejectsJournalReplacement(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	repository := openFixtureRepository(t, root)
	defer func() { _ = repository.Close() }()
	threadID := mustEnumerationThreadID(t, "2024-03-04T00:00:00Z", 4)
	lease, err := repository.Create(context.Background(), threadID)
	requireNoError(t, err)
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	journalPath, err := repository.JournalPath(threadID)
	requireNoError(t, err)
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
	requireNoError(t, err)
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
	requireNoError(t, err)
	return threadID
}
