//go:build darwin || linux

package catalog

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/session"
)

func TestCatalogReconcileAndRebuildFromJournals(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	catalogDirectory := privateTempDir(t)
	databasePath := filepath.Join(catalogDirectory, "state.sqlite")
	repository, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	cwd := t.TempDir()
	threadIDs := make([]domain.ThreadID, 0, 2)
	for index := range 2 {
		identity, generateErr := session.GenerateRootIdentity()
		if generateErr != nil {
			t.Fatal(generateErr)
		}
		writer, _, createErr := session.CreateRootJournal(context.Background(), repository, identity, session.RootJournalConfig{
			Provider: domain.ProviderOpenAI, ProviderWire: "responses", Model: "gpt-test",
			CreationCWD: cwd, CreatedAt: time.Unix(int64(index+1), 0).UTC(),
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if closeErr := writer.Close(context.Background()); closeErr != nil {
			t.Fatal(closeErr)
		}
		threadIDs = append(threadIDs, identity.ThreadID)
	}
	selector := Selector{
		CreationCWD: cwd, ProviderFamily: domain.ProviderOpenAI,
		ProviderWire: "responses", Model: "gpt-test",
	}
	first := reconcileAndSelect(t, databasePath, root, repository, selector)
	for _, threadID := range threadIDs {
		journalPath, pathErr := repository.JournalPath(threadID)
		if pathErr != nil {
			t.Fatal(pathErr)
		}
		mtime := time.Unix(900, 0)
		if threadID == first.ThreadID {
			mtime = time.Unix(10, 0)
		}
		if err := os.Chtimes(journalPath, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(databasePath); err != nil {
		t.Fatal(err)
	}
	second := reconcileAndSelect(t, databasePath, root, repository, selector)
	if first.ThreadID != second.ThreadID || first.SessionID != second.SessionID || first.LastChecksum != second.LastChecksum {
		t.Fatalf("rebuilt entry differs: first=%#v second=%#v", first, second)
	}
	if first.ThreadID != threadIDs[1] && first.ThreadID != threadIDs[0] {
		t.Fatalf("selected unknown thread: %s", first.ThreadID)
	}
}

func TestCatalogLatestCompatibleUsesExactFilterAndTieBreak(t *testing.T) {
	directory := privateTempDir(t)
	instance := openTestCatalog(t, filepath.Join(directory, "state.sqlite"), filepath.Join(directory, "sessions"))
	defer func() { _ = instance.Close() }()
	cwd := t.TempDir()
	updatedAt := time.Unix(500, 0).UTC()
	entries := []Entry{
		catalogTestEntry(t, cwd, domain.ProviderOpenAI, "responses", "gpt-test", updatedAt),
		catalogTestEntry(t, cwd, domain.ProviderOpenAI, "responses", "gpt-test", updatedAt),
		catalogTestEntry(t, cwd, domain.ProviderAnthropic, "messages", "claude-test", updatedAt.Add(time.Hour)),
	}
	present := make(map[domain.ThreadID]struct{}, len(entries))
	for _, entry := range entries {
		present[entry.ThreadID] = struct{}{}
	}
	if err := instance.applyReconciliation(context.Background(), entries, present, nil); err != nil {
		t.Fatal(err)
	}
	entry, found, err := instance.LatestCompatible(context.Background(), Selector{
		CreationCWD: cwd, ProviderFamily: domain.ProviderOpenAI, ProviderWire: "responses", Model: "gpt-test",
	})
	if err != nil || !found {
		t.Fatalf("LatestCompatible() = %#v, %v, %v", entry, found, err)
	}
	want := []string{string(entries[0].ThreadID), string(entries[1].ThreadID)}
	sort.Strings(want)
	if string(entry.ThreadID) != want[1] {
		t.Fatalf("tie winner = %s, want %s", entry.ThreadID, want[1])
	}
	_, found, err = instance.LatestCompatible(context.Background(), Selector{
		CreationCWD: cwd, ProviderFamily: domain.ProviderOpenAI, ProviderWire: "responses", Model: "other",
	})
	if err != nil || found {
		t.Fatalf("incompatible query found=%v err=%v", found, err)
	}
}

func TestCatalogTransactionRollsBackAtomically(t *testing.T) {
	directory := privateTempDir(t)
	instance := openTestCatalog(t, filepath.Join(directory, "state.sqlite"), filepath.Join(directory, "sessions"))
	defer func() { _ = instance.Close() }()
	cwd := t.TempDir()
	first := catalogTestEntry(t, cwd, domain.ProviderOpenAI, "responses", "gpt-test", time.Unix(500, 0).UTC())
	second := catalogTestEntry(t, cwd, domain.ProviderOpenAI, "responses", "gpt-test", time.Unix(600, 0).UTC())
	second.JournalPath = first.JournalPath
	present := map[domain.ThreadID]struct{}{first.ThreadID: {}, second.ThreadID: {}}
	if err := instance.applyReconciliation(context.Background(), []Entry{first, second}, present, nil); err == nil {
		t.Fatal("applyReconciliation() unexpectedly committed duplicate journal paths")
	}
	_, found, err := instance.LatestCompatible(context.Background(), Selector{
		CreationCWD: cwd, ProviderFamily: domain.ProviderOpenAI, ProviderWire: "responses", Model: "gpt-test",
	})
	if err != nil || found {
		t.Fatalf("rolled-back query found=%v err=%v", found, err)
	}
}

func TestCatalogRebuildsImmutableCorruptAndIncompatibleFixtures(t *testing.T) {
	for _, fixture := range []string{"corrupt.sqlite", "incompatible.sqlite"} {
		t.Run(fixture, func(t *testing.T) {
			directory := privateTempDir(t)
			root := filepath.Join(directory, "sessions")
			databasePath := filepath.Join(directory, "state.sqlite")
			repository, err := session.OpenOrCreateRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = repository.Close() }()
			cwd := t.TempDir()
			threadID := createCatalogJournal(t, repository, cwd)
			journalPath, err := repository.JournalPath(threadID)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			fixtureBytes, err := os.ReadFile(filepath.Join("testdata", "catalog", fixture))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(databasePath, fixtureBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			instance := openTestCatalog(t, databasePath, root)
			if _, err := instance.Reconcile(context.Background(), repository); err != nil {
				t.Fatal(err)
			}
			entry, found, err := instance.LatestCompatible(context.Background(), Selector{
				CreationCWD: cwd, ProviderFamily: domain.ProviderOpenAI, ProviderWire: "responses", Model: "gpt-test",
			})
			if err != nil || !found || entry.ThreadID != threadID {
				t.Fatalf("rebuilt entry=%#v found=%v err=%v", entry, found, err)
			}
			if err := instance.Close(); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(journalPath)
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("fixture rebuild changed journal: %v", err)
			}
		})
	}
}

func TestCatalogSchemaAndFilesExcludeSecretsAndUsePrivatePermissions(t *testing.T) {
	directory := privateTempDir(t)
	databasePath := filepath.Join(directory, "state.sqlite")
	instance := openTestCatalog(t, databasePath, filepath.Join(directory, "sessions"))
	cwd := t.TempDir()
	entry := catalogTestEntry(t, cwd, domain.ProviderOpenAI, "responses", "gpt-test", time.Now().UTC())
	if err := instance.applyReconciliation(
		context.Background(), []Entry{entry}, map[domain.ThreadID]struct{}{entry.ThreadID: {}}, nil,
	); err != nil {
		t.Fatal(err)
	}
	schema, err := querySingleText(instance.db, "SELECT group_concat(sql, ';') FROM sqlite_schema WHERE sql IS NOT NULL")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"title", "tag", "prompt", "payload", "base_url", "api_key", "authorization"} {
		if strings.Contains(strings.ToLower(schema), forbidden) {
			t.Fatalf("schema contains deferred or sensitive field %q: %s", forbidden, schema)
		}
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	if err := instance.Close(); err != nil {
		t.Fatalf("second Close() = %v", err)
	}
	for _, path := range []string{databasePath, databasePath + ".lock"} {
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

func TestCatalogRejectsSymlinkedDatabaseAndSidecar(t *testing.T) {
	t.Run("database", func(t *testing.T) {
		directory := privateTempDir(t)
		target := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(target, []byte("outside-secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(directory, "state.sqlite")); err != nil {
			t.Fatal(err)
		}
		instance, err := New(Config{DatabasePath: filepath.Join(directory, "state.sqlite"), SessionRoot: filepath.Join(directory, "sessions")})
		if err != nil {
			t.Fatal(err)
		}
		if err := instance.Open(context.Background()); err == nil {
			t.Fatal("Open() unexpectedly followed database symlink")
		}
		assertFileContent(t, target, "outside-secret")
	})
	t.Run("rollback journal", func(t *testing.T) {
		directory := privateTempDir(t)
		databasePath := filepath.Join(directory, "state.sqlite")
		instance := openTestCatalog(t, databasePath, filepath.Join(directory, "sessions"))
		if err := instance.Close(); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(target, []byte("outside-secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, databasePath+"-journal"); err != nil {
			t.Fatal(err)
		}
		instance, err := New(Config{DatabasePath: databasePath, SessionRoot: filepath.Join(directory, "sessions")})
		if err != nil {
			t.Fatal(err)
		}
		if err := instance.Open(context.Background()); err == nil {
			t.Fatal("Open() unexpectedly accepted sidecar symlink")
		}
		assertFileContent(t, target, "outside-secret")
	})
}

func reconcileAndSelect(t *testing.T, databasePath string, root string, repository *session.Repository, selector Selector) Entry {
	t.Helper()
	instance := openTestCatalog(t, databasePath, root)
	defer func() { _ = instance.Close() }()
	report, err := instance.Reconcile(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid != 2 || report.Invalid != 0 || report.Busy != 0 {
		t.Fatalf("report = %#v", report)
	}
	entry, found, err := instance.LatestCompatible(context.Background(), selector)
	if err != nil || !found {
		t.Fatalf("LatestCompatible() = %#v, %v, %v", entry, found, err)
	}
	return entry
}

func openTestCatalog(t *testing.T, databasePath string, root string) *Catalog {
	t.Helper()
	instance, err := New(Config{DatabasePath: databasePath, SessionRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if err := instance.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
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
