//go:build darwin || linux

package catalog

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/session"
)

func TestReconcileDeletesMissingAndExcludesInvalidRows(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "missing", mutate: func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "invalid", mutate: func(t *testing.T, path string) {
			file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.WriteString("invalid-middle-record\n"); err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := privateTempDir(t)
			root := filepath.Join(directory, "sessions")
			repository, err := session.OpenOrCreateRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = repository.Close() }()
			cwd := t.TempDir()
			threadID := createCatalogJournal(t, repository, cwd)
			instance := openTestCatalog(t, filepath.Join(directory, "state.sqlite"), root)
			defer func() { _ = instance.Close() }()
			if _, err := instance.Reconcile(context.Background(), repository); err != nil {
				t.Fatal(err)
			}
			path, err := repository.JournalPath(threadID)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(t, path)
			before, _ := os.ReadFile(path)
			report, err := instance.Reconcile(context.Background(), repository)
			if err != nil {
				t.Fatal(err)
			}
			if test.name == "invalid" && report.Invalid != 1 {
				t.Fatalf("invalid report = %#v", report)
			}
			_, found, err := instance.LatestCompatible(context.Background(), Selector{
				CreationCWD: cwd, ProviderFamily: domain.ProviderOpenAI, ProviderWire: "responses", Model: "gpt-test",
			})
			if err != nil || found {
				t.Fatalf("stale row found=%v err=%v", found, err)
			}
			if test.name == "invalid" {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("invalid journal changed: %v", err)
				}
				lease, err := repository.Open(context.Background(), threadID)
				if err != nil {
					t.Fatalf("invalid path retained lease: %v", err)
				}
				_ = lease.Close()
			}
		})
	}
}

func TestReconcileRepairsRecoverableTailUnderLease(t *testing.T) {
	directory := privateTempDir(t)
	root := filepath.Join(directory, "sessions")
	repository, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	cwd := t.TempDir()
	threadID := createCatalogJournal(t, repository, cwd)
	path, err := repository.JournalPath(threadID)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"schema_version":1`); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	instance := openTestCatalog(t, filepath.Join(directory, "state.sqlite"), root)
	defer func() { _ = instance.Close() }()
	report, err := instance.Reconcile(context.Background(), repository)
	if err != nil || report.Valid != 1 {
		t.Fatalf("repair report = %#v, %v", report, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, committed) {
		t.Fatalf("repaired bytes differ: %v", err)
	}
	entry, found, err := instance.LatestCompatible(context.Background(), Selector{
		CreationCWD: cwd, ProviderFamily: domain.ProviderOpenAI, ProviderWire: "responses", Model: "gpt-test",
	})
	if err != nil || !found || entry.LastSequence != 2 {
		t.Fatalf("repaired entry=%#v found=%v err=%v", entry, found, err)
	}
}

func BenchmarkCatalogReconcile(b *testing.B) {
	directory := privateBenchmarkDir(b)
	root := filepath.Join(directory, "sessions")
	repository, err := session.OpenOrCreateRepository(root)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	cwd := b.TempDir()
	for range 25 {
		createCatalogJournalForBenchmark(b, repository, cwd)
	}
	instance, err := New(Config{DatabasePath: filepath.Join(directory, "state.sqlite"), SessionRoot: root})
	if err != nil {
		b.Fatal(err)
	}
	if err := instance.Open(context.Background()); err != nil {
		b.Fatal(err)
	}
	defer func() { _ = instance.Close() }()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := instance.Reconcile(context.Background(), repository); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRepeatedReconcileDoesNotLeaveBackgroundGoroutines(t *testing.T) {
	directory := privateTempDir(t)
	root := filepath.Join(directory, "sessions")
	repository, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	cwd := t.TempDir()
	for range 3 {
		createCatalogJournal(t, repository, cwd)
	}
	instance := openTestCatalog(t, filepath.Join(directory, "state.sqlite"), root)
	defer func() { _ = instance.Close() }()
	if _, err := instance.Reconcile(context.Background(), repository); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.Gosched()
	before := runtime.NumGoroutine()
	for range 10 {
		if _, err := instance.Reconcile(context.Background(), repository); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	runtime.Gosched()
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("reconcile left background goroutines: before=%d after=%d", before, after)
	}
}

func privateBenchmarkDir(b *testing.B) string {
	b.Helper()
	directory := b.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		b.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		b.Fatal(err)
	}
	return resolved
}

func createCatalogJournalForBenchmark(b *testing.B, repository *session.Repository, cwd string) {
	b.Helper()
	identity, err := session.GenerateRootIdentity()
	if err != nil {
		b.Fatal(err)
	}
	writer, _, err := session.CreateRootJournal(context.Background(), repository, identity, session.RootJournalConfig{
		Provider: domain.ProviderOpenAI, ProviderWire: "responses", Model: "gpt-test", CreationCWD: cwd,
	})
	if err != nil {
		b.Fatal(err)
	}
	if err := writer.Close(context.Background()); err != nil {
		b.Fatal(err)
	}
}
