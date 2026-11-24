//go:build darwin || linux

package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"easycode/internal/domain"
)

func TestCatalogCanceledPublicationKeepsPreviousSnapshot(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(privateTempDir(t), "catalog.json")
	instance := openTestCatalog(t, path)
	cwd := t.TempDir()
	first := catalogTestEntry(t, cwd, domain.ProviderOpenAI, "responses", "gpt-test", time.Unix(500, 0).UTC())
	if err := instance.publish(ctx, []Entry{first}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.Title = "Unpublished title"
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := instance.publish(canceled, []Entry{second}); !errors.Is(err, context.Canceled) {
		t.Fatalf("publish() error = %v, want context.Canceled", err)
	}
	assertFileContent(t, path, string(before))
	entry, found, err := instance.LatestCompatible(ctx, Selector{
		CreationCWD: cwd, ProviderFamily: domain.ProviderOpenAI, ProviderWire: "responses", Model: "gpt-test",
	})
	if err != nil || !found || entry != first {
		t.Fatalf("query after rejected publication = %+v, %v, %v", entry, found, err)
	}
}

func TestCatalogSnapshotExcludesSecretsAndUsesPrivatePermissions(t *testing.T) {
	directory := privateTempDir(t)
	path := filepath.Join(directory, "catalog.json")
	instance := openTestCatalog(t, path)
	cwd := t.TempDir()
	entry := catalogTestEntry(t, cwd, domain.ProviderOpenAI, "responses", "gpt-test", time.Now().UTC())
	if err := instance.publish(context.Background(), []Entry{entry}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(content, &rows); err != nil || len(rows) != 1 {
		t.Fatalf("persisted snapshot rows = %d, %v", len(rows), err)
	}
	fields := []string{"session_id", "thread_id", "journal_path", "created_at", "updated_at", "last_sequence",
		"last_checksum", "creation_cwd", "provider_family", "provider_wire", "model", "title"}
	if len(rows[0]) != len(fields) {
		t.Fatalf("persisted snapshot has %d fields, want %d", len(rows[0]), len(fields))
	}
	for _, field := range fields {
		if _, ok := rows[0][field]; !ok {
			t.Fatalf("persisted snapshot is missing %q", field)
		}
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	if err := instance.Close(); err != nil {
		t.Fatalf("second Close() = %v", err)
	}
	for _, path := range []string{path, path + ".lock"} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("inspect catalog file %s: %v", filepath.Base(path), err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("catalog file %s mode=%v", filepath.Base(path), info.Mode().Perm())
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"api-secret", "Authorization", "https://secret.example", "prompt-secret", "response-secret"} {
			if bytes.Contains(content, []byte(secret)) {
				t.Fatalf("catalog file contains secret %q", secret)
			}
		}
	}
}

func TestCatalogRejectsUnsafeSnapshotOrLock(t *testing.T) {
	for _, suffix := range []string{"", ".lock"} {
		t.Run("symlink"+suffix, func(t *testing.T) {
			path := filepath.Join(privateTempDir(t), "catalog.json")
			target := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(target, []byte("outside-secret"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path+suffix); err != nil {
				t.Fatal(err)
			}
			instance, err := New(Config{SnapshotPath: path})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = instance.Close() })
			if err := instance.Open(context.Background()); err == nil {
				t.Fatal("Open() unexpectedly accepted a symlink")
			}
			assertFileContent(t, target, "outside-secret")
		})
	}

	t.Run("non-private snapshot", func(t *testing.T) {
		directory := privateTempDir(t)
		path := filepath.Join(directory, "catalog.json")
		if err := os.WriteFile(path, []byte("unsafe snapshot"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		instance, err := New(Config{SnapshotPath: path})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = instance.Close() })
		if err := instance.Open(context.Background()); err == nil {
			t.Fatal("Open() unexpectedly accepted non-private snapshot")
		}
		assertFileContent(t, path, "unsafe snapshot")
	})
}

func TestCatalogRejectsReplacedSnapshotBeforePublishing(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(privateTempDir(t), "catalog.json")
	instance := openTestCatalog(t, path)
	entry := catalogTestEntry(t, t.TempDir(), domain.ProviderOpenAI, "responses", "gpt-test", time.Unix(500, 0).UTC())
	if err := instance.publish(ctx, []Entry{entry}); err != nil {
		t.Fatal(err)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	if err := instance.Open(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	previous := path + ".old"
	if err := os.Rename(path, previous); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("foreign snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := instance.publish(ctx, []Entry{entry}); err == nil {
		t.Fatal("publish() unexpectedly accepted a replaced snapshot with unchanged entries")
	}
	updated := entry
	updated.Title = "Unpublished title"
	if err := instance.publish(ctx, []Entry{updated}); err == nil {
		t.Fatal("publish() unexpectedly replaced a distinct inode at the snapshot path")
	}
	assertFileContent(t, path, "foreign snapshot")
	assertFileContent(t, previous, string(before))
	result, err := instance.List(ctx, ListQuery{Scope: ListAllDirectories, Limit: 2})
	if err != nil || len(result.Entries) != 1 || result.Entries[0] != entry {
		t.Fatalf("query after rejected publication = %+v, %v", result, err)
	}
}

func openTestCatalog(t *testing.T, path string) *Catalog {
	t.Helper()
	instance, err := New(Config{SnapshotPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := instance.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	return instance
}

func catalogTestEntry(t *testing.T, cwd string, family domain.ProviderFamily, wire string, model string, updatedAt time.Time) Entry {
	t.Helper()
	identity := catalogTestIdentity(t)
	return Entry{
		SessionID: identity.SessionID, ThreadID: identity.ThreadID,
		JournalPath: catalogTestJournalPath(t, identity.ThreadID),
		CreatedAt:   updatedAt.Add(-time.Second), UpdatedAt: updatedAt,
		LastSequence: 2, LastChecksum: "checksum-" + string(identity.ThreadID),
		CreationCWD: cwd, ProviderFamily: family, ProviderWire: wire, Model: model,
	}
}

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != want {
		t.Fatalf("content = %q, want %q", content, want)
	}
}

func privateTempDir(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestCatalogSnapshotValidatesBranchParentAndSeparatesThreadIdentity(t *testing.T) {
	ctx := context.Background()
	root := catalogTestEntry(t, t.TempDir(), domain.ProviderOpenAI, "responses", "gpt-test", time.Unix(500, 0).UTC())
	child := catalogTestEntry(t, root.CreationCWD, root.ProviderFamily, root.ProviderWire, root.Model, time.Unix(501, 0).UTC())
	child.SessionID = root.SessionID
	child.ParentThreadID = parentThreadID(root.ThreadID)
	child.Title = "Alternative"
	var file bytes.Buffer
	if err := writeSnapshot(ctx, &file, []Entry{child, root}); err != nil {
		t.Fatal(err)
	}
	entries, err := decodeSnapshot(ctx, bytes.NewReader(file.Bytes()))
	if err != nil || len(entries) != 2 || entries[0] != child || entries[1] != root {
		t.Fatalf("root and branch snapshot did not round trip: %+v, %v", entries, err)
	}
	needle := []byte(fmt.Sprintf(`"parent_thread_id":%q`, root.ThreadID))
	for _, invalid := range []string{`"parent_thread_id":""`, `"parent_thread_id":null`, fmt.Sprintf(`"parent_thread_id":%q`, child.ThreadID)} {
		data := bytes.Replace(file.Bytes(), needle, []byte(invalid), 1)
		if _, err := decodeSnapshot(ctx, bytes.NewReader(data)); !errors.Is(err, errInvalidSnapshot) {
			t.Fatalf("invalid branch reference %s accepted: %v", invalid, err)
		}
	}
}
