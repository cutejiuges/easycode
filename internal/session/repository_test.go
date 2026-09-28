package session

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestRepositoryCreatesPrivateJournalAtUUIDv7Date(t *testing.T) {
	t.Parallel()
	dataRoot := filepath.Join(t.TempDir(), "sessions")
	repository, err := NewRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })

	file, err := repository.Create(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dataRoot, "1970", "01", "01", string(testThreadID)+".jsonl")
	got, err := repository.JournalPath(testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("JournalPath() = %q, want %q", got, want)
	}
	assertMode(t, dataRoot, 0o700)
	assertMode(t, filepath.Dir(want), 0o700)
	assertMode(t, want, 0o600)

	reopened, err := repository.Open(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Create(context.Background(), testThreadID); err == nil {
		t.Fatal("Create() unexpectedly replaced an existing journal")
	}
}

func TestRepositoryRejectsInvalidIDAndCancelledContext(t *testing.T) {
	t.Parallel()
	repository, err := NewRepository(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	for _, threadID := range []domain.ThreadID{"../escape", "/absolute", "not-a-uuid"} {
		if _, err := repository.Create(context.Background(), threadID); err == nil {
			t.Fatalf("Create(%q) unexpectedly succeeded", threadID)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.Create(ctx, testThreadID); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("Create(cancelled) error = %v", err)
	}
}

func TestRepositoryRejectsSymlinksAndNonRegularTargets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires Unix permissions")
	}
	dataRoot := filepath.Join(t.TempDir(), "sessions")
	repository, err := NewRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dataRoot, "1970")); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Create(context.Background(), testThreadID); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("Create() through symlink error = %v", err)
	}
	_ = repository.Close()

	otherRoot := filepath.Join(t.TempDir(), "sessions")
	other, err := NewRepository(otherRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	target, err := other.JournalPath(testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Open(context.Background(), testThreadID); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("Open(directory) error = %v", err)
	}
}

func TestRepositoryRejectsUnsafeExistingPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission fixture is not meaningful on Windows")
	}
	parent := t.TempDir()
	unsafeRoot := filepath.Join(parent, "unsafe")
	if err := os.Mkdir(unsafeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRepository(unsafeRoot); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("NewRepository() error = %v", err)
	}

	dataRoot := filepath.Join(parent, "sessions")
	repository, err := NewRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	target, err := repository.JournalPath(testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Open(context.Background(), testThreadID); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("Open() error = %v", err)
	}
}

func TestRepositoryRejectsSymlinkDataRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires Unix permissions")
	}
	outside := t.TempDir()
	rootLink := filepath.Join(t.TempDir(), "sessions")
	if err := os.Symlink(outside, rootLink); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRepository(rootLink); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("NewRepository(symlink) error = %v", err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode(%q) = %o, want %o", path, got, want)
	}
}
