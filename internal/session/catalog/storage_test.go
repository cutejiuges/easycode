//go:build darwin || linux

package catalog

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
)

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
