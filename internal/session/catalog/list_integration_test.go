//go:build darwin || linux

package catalog

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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
