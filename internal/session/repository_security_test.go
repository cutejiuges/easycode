//go:build darwin || linux

package session

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRepositoryTOCTOURejectsDataRootSymlinkBeforeOpen(t *testing.T) {
	parent := t.TempDir()
	rootPath := filepath.Join(parent, "sessions")
	outside := t.TempDir()
	outsideMarker := filepath.Join(outside, "marker")
	writeSecurityFixture(t, outsideMarker, []byte("outside"), 0o600)
	repository, err := NewRepository(rootPath)
	requireNoError(t, err)
	repository.hooks.beforeOpenRoot = func(string) {
		if err := os.Symlink(outside, rootPath); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.OpenOrCreate(); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("OpenOrCreate() error = %v", err)
	}
	assertSecurityFixture(t, outsideMarker, []byte("outside"))
}

func TestRepositoryTOCTOURemainsBoundToOpenedDataRoot(t *testing.T) {
	parent := t.TempDir()
	rootPath := filepath.Join(parent, "sessions")
	if err := os.Mkdir(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	heldRoot := filepath.Join(parent, "opened-sessions")
	outside := t.TempDir()
	outsideMarker := filepath.Join(outside, "marker")
	writeSecurityFixture(t, outsideMarker, []byte("outside"), 0o600)
	repository, err := NewRepository(rootPath)
	requireNoError(t, err)
	repository.hooks.afterOpenRoot = func(string) {
		if err := os.Rename(rootPath, heldRoot); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, rootPath); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.OpenOrCreate(); err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	lease, err := repository.Create(context.Background(), testThreadID)
	requireNoError(t, err)
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	heldJournal := filepath.Join(heldRoot, "1970", "01", "01", string(testThreadID)+".jsonl")
	if info, err := os.Stat(heldJournal); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("opened data root did not receive journal: %#v, %v", info, err)
	}
	assertSecurityFixture(t, outsideMarker, []byte("outside"))
}

func TestRepositoryRejectsSymlinkedJournalWithoutChangingTarget(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "sessions")
	repository := openFixtureRepository(t, rootPath)
	defer repository.Close()
	journalPath, err := repository.JournalPath(testThreadID)
	requireNoError(t, err)
	writeSecurityDirectory(t, filepath.Dir(journalPath))
	outsideJournal := filepath.Join(t.TempDir(), "outside.jsonl")
	outsideBytes := []byte("outside-journal-bytes")
	writeSecurityFixture(t, outsideJournal, outsideBytes, 0o600)
	if err := os.Symlink(outsideJournal, journalPath); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Open(context.Background(), testThreadID); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("Open(symlink) error = %v", err)
	}
	assertSecurityFixture(t, outsideJournal, outsideBytes)
}

func TestRepositoryResumeRepairAndWriterStayBoundToOpenedJournal(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "sessions")
	repository := openFixtureRepository(t, rootPath)
	defer repository.Close()
	lease, err := repository.Create(context.Background(), testThreadID)
	requireNoError(t, err)
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	journalPath, err := repository.JournalPath(testThreadID)
	requireNoError(t, err)
	committed := encodeTestBatch(t, 1, []RecordDraft{
		mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}),
	})
	torn := append(append([]byte(nil), committed...), []byte(`{"seq":1`)...)
	writeSecurityFixture(t, journalPath, torn, 0o600)
	heldJournal := journalPath + ".opened"
	outsideJournal := filepath.Join(t.TempDir(), "outside.jsonl")
	outsideBytes := []byte("outside-journal-bytes")
	writeSecurityFixture(t, outsideJournal, outsideBytes, 0o600)
	var swap sync.Once
	repository.root.hooks.afterOpenJournal = func(string) {
		swap.Do(func() {
			if err := os.Rename(journalPath, heldJournal); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outsideJournal, journalPath); err != nil {
				t.Fatal(err)
			}
		})
	}
	lease, err = repository.Open(context.Background(), testThreadID)
	requireNoError(t, err)
	loaded := loadFixtureJournal(t, context.Background(), lease)
	loaded.Repair, err = NewLoader().Repair(context.Background(), lease, loaded)
	requireNoError(t, err)
	if !loaded.Repair.Repaired || loaded.Repair.Kind != RepairHalfLine {
		t.Fatalf("repair = %#v", loaded.Repair)
	}
	writer, err := StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
	requireNoError(t, err)
	failedDraft := mustDraft(t, EventTurnFailed, testTurnID, TurnFailedPayload{Code: "failed"})
	if _, err := writer.AppendBatch(context.Background(), []RecordDraft{failedDraft}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertSecurityFixture(t, outsideJournal, outsideBytes)
	heldBytes := readSessionFixtureFile(t, heldJournal)
	if !bytes.HasPrefix(heldBytes, committed) || bytes.Count(heldBytes, []byte(`"event_kind"`)) != 2 || heldBytes[len(heldBytes)-1] != '\n' {
		t.Fatalf("opened journal repair/append bytes are invalid: %s", heldBytes)
	}
}

func writeSecurityDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func writeSecurityFixture(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	writeSecurityDirectory(t, filepath.Dir(path))
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func assertSecurityFixture(t *testing.T, path string, want []byte) {
	t.Helper()
	got := readSessionFixtureFile(t, path)
	if !bytes.Equal(got, want) {
		t.Fatalf("external fixture changed: got %q want %q", got, want)
	}
}
