//go:build darwin || linux

package catalog

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"easycode/internal/session"
)

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
