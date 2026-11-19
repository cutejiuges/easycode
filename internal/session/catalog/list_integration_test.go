//go:build darwin || linux

package catalog

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/session"
)

func TestListBusyProcessCannotRepairUntilRelease(t *testing.T) {
	dir := privateTempDir(t)
	root := filepath.Join(dir, "sessions")
	repository, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	first := createCatalogJournal(t, repository, dir)
	instance := openTestCatalog(t, filepath.Join(dir, "state.sqlite"), root)
	defer func() { _ = instance.Close() }()
	if _, err := instance.Reconcile(context.Background(), repository); err != nil {
		t.Fatal(err)
	}
	second := createCatalogJournal(t, repository, dir)
	paths := make([]string, 0, 2)
	committed := make([][]byte, 0, 2)
	for _, id := range []domain.ThreadID{first, second} {
		path, err := repository.JournalPath(id)
		if err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
		committed = append(committed, content)
		if err := os.WriteFile(path, append(append([]byte(nil), content...), []byte(`{"incomplete":`)...), 0600); err != nil {
			t.Fatal(err)
		}
	}
	one := startCatalogProcessHelper(t, catalogJournalHelperEnv, "", root, string(first))
	two := startCatalogProcessHelper(t, catalogJournalHelperEnv, "", root, string(second))
	report, err := instance.Reconcile(context.Background(), repository)
	if err != nil || report.Busy != 2 || !report.BusyUnindexed {
		t.Fatalf("report=%+v %v", report, err)
	}
	result, err := instance.List(context.Background(), ListQuery{Scope: ListAllDirectories, Limit: 20})
	if err != nil || len(result.Entries) != 1 || result.Entries[0].ThreadID != first || result.Entries[0].LastSequence != 2 {
		t.Fatalf("busy=%+v %v", result, err)
	}
	for i, path := range paths {
		got, err := os.ReadFile(path)
		want := append(append([]byte(nil), committed[i]...), []byte(`{"incomplete":`)...)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("busy journal repaired")
		}
	}
	one.stop(t)
	two.kill(t)
	report, err = instance.Reconcile(context.Background(), repository)
	if err != nil || report.Busy != 0 || report.Valid != 2 {
		t.Fatalf("release=%+v %v", report, err)
	}
	result, err = instance.List(context.Background(), ListQuery{Scope: ListAllDirectories, Limit: 20})
	if err != nil || len(result.Entries) != 2 {
		t.Fatalf("release list=%+v %v", result, err)
	}
	for i, path := range paths {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, append(append([]byte(nil), committed[i]...), []byte(`{"incomplete":`)...)) {
			t.Fatal("read-only catalog modified journal")
		}
	}
}

func TestCatalogTitleSnapshotUpdatesListAndLatestCompatible(t *testing.T) {
	ctx := context.Background()
	dir := privateTempDir(t)
	root := filepath.Join(dir, "sessions")
	repository, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	database := openTestCatalog(t, filepath.Join(dir, "state.sqlite"), root)
	defer database.Close()
	older := createCatalogJournal(t, repository, dir)
	observed := appendCatalogTitle(t, repository, older, "Observed title")
	newer := createCatalogJournal(t, repository, dir)
	query := ListQuery{Scope: ListAllDirectories, Limit: 2}
	selector := Selector{CreationCWD: dir, ProviderFamily: domain.ProviderOpenAI, ProviderWire: "responses", Model: "gpt-test"}
	if _, err := database.Reconcile(ctx, repository); err != nil {
		t.Fatal(err)
	}
	first, err := database.List(ctx, query)
	if err != nil || len(first.Entries) != 2 || first.Entries[0].ThreadID != newer || first.Entries[1].ThreadID != older {
		t.Fatalf("initial list = %+v, %v", first, err)
	}
	assertCatalogTitle(t, first.Entries[1], "Observed title", observed)
	committed := appendCatalogTitle(t, repository, older, "Newly committed title")

	// 按协调的两个阶段交错操作，模拟读完释放 lease 后、SQLite 发布前的改名。
	present := map[domain.ThreadID]struct{}{older: {}, newer: {}}
	if err := database.applyReconciliation(ctx, first.Entries, present, nil); err != nil {
		t.Fatal(err)
	}
	stale, err := database.List(ctx, query)
	if err != nil || !slices.Equal(stale.Entries, first.Entries) {
		t.Fatalf("observed snapshot = %+v, %v", stale, err)
	}
	latest, found, err := database.LatestCompatible(ctx, selector)
	if err != nil || !found || latest.ThreadID != newer {
		t.Fatalf("latest compatible snapshot = %+v, %v, %v", latest, found, err)
	}
	if _, err := database.Reconcile(ctx, repository); err != nil {
		t.Fatal(err)
	}
	updated, err := database.List(ctx, query)
	if err != nil || len(updated.Entries) != 2 || updated.Entries[0].ThreadID != older {
		t.Fatalf("refreshed list = %+v, %v", updated, err)
	}
	assertCatalogTitle(t, updated.Entries[0], "Newly committed title", committed)
	latest, found, err = database.LatestCompatible(ctx, selector)
	if err != nil || !found || latest != updated.Entries[0] {
		t.Fatalf("latest compatible after rename = %+v, %v, %v", latest, found, err)
	}
}

func TestCatalogRebuildsMismatchedTitleField(t *testing.T) {
	ctx := context.Background()
	dir := privateTempDir(t)
	root := filepath.Join(dir, "sessions")
	repository, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	untitled := createCatalogJournal(t, repository, dir)
	named := createCatalogJournal(t, repository, dir)
	committed := appendCatalogTitle(t, repository, named, "解析 👩‍💻")
	paths := make(map[string][]byte, 2)
	for _, thread := range []domain.ThreadID{untitled, named} {
		path, err := repository.JournalPath(thread)
		if err != nil {
			t.Fatal(err)
		}
		pathBytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		paths[path] = pathBytes
	}
	databasePath := filepath.Join(dir, "state.sqlite")
	database := openTestCatalog(t, databasePath, root)
	defer database.Close()
	for _, scenario := range []struct {
		name, change string
	}{
		{name: "missing field", change: "ALTER TABLE threads DROP COLUMN title"},
		{name: "wrong type", change: "ALTER TABLE threads DROP COLUMN title; ALTER TABLE threads ADD COLUMN title INTEGER NOT NULL DEFAULT 0"},
		{name: "nullable field", change: "ALTER TABLE threads DROP COLUMN title; ALTER TABLE threads ADD COLUMN title TEXT"},
		{name: "deleted database"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if scenario.change != "" {
				if err := database.db.Exec(scenario.change); err != nil {
					t.Fatal(err)
				}
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			if scenario.change == "" {
				if err := os.Remove(databasePath); err != nil {
					t.Fatal(err)
				}
			}
			if err := database.Open(ctx); err != nil {
				t.Fatal(err)
			}
			schema, err := querySingleText(database.db, `SELECT type || ':' || "notnull" FROM pragma_table_info('threads') WHERE name = 'title'`)
			if err != nil || schema != "TEXT:1" {
				t.Fatalf("rebuilt title schema = %q, %v", schema, err)
			}
			if _, err := database.Reconcile(ctx, repository); err != nil {
				t.Fatal(err)
			}
			result, err := database.List(ctx, ListQuery{Scope: ListAllDirectories, Limit: 2})
			if err != nil || len(result.Entries) != 2 || result.Entries[0].ThreadID != named || result.Entries[1].ThreadID != untitled {
				t.Fatalf("rebuilt list = %+v, %v", result, err)
			}
			assertCatalogTitle(t, result.Entries[0], "解析 👩‍💻", committed)
			if result.Entries[0].Model != "gpt-test" || result.Entries[1].Title != "" || result.Entries[1].LastSequence != 2 {
				t.Fatalf("rebuilt metadata = %+v", result.Entries)
			}
		})
		if t.Failed() {
			return
		}
	}
	for path, want := range paths {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("catalog rebuild changed journal: %v", err)
		}
	}
}

func appendCatalogTitle(t *testing.T, repository *session.Repository, thread domain.ThreadID, title domain.SessionTitle) session.Record {
	t.Helper()
	ctx := context.Background()
	draft, err := session.NewSessionTitleUpdatedDraft(title)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := repository.Open(ctx, thread)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := session.NewLoader().Load(ctx, lease)
	if err != nil {
		_ = lease.Close()
		t.Fatal(err)
	}
	writer, err := session.StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
	if err != nil {
		_ = lease.Close()
		t.Fatal(err)
	}
	records, err := writer.AppendBatch(ctx, []session.RecordDraft{draft})
	closeErr := writer.Close(ctx)
	if err != nil || closeErr != nil {
		t.Fatalf("write title: %v; close: %v", err, closeErr)
	}
	return records[0]
}

func assertCatalogTitle(t *testing.T, entry Entry, title domain.SessionTitle, record session.Record) {
	t.Helper()
	if entry.Title != title || entry.ThreadID != record.ThreadID || entry.LastSequence != record.Sequence ||
		entry.LastChecksum != record.Checksum || !entry.UpdatedAt.Equal(record.Timestamp) {
		t.Fatalf("title or ordering evidence does not match record: %+v", entry)
	}
}
