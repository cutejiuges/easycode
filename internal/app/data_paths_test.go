package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDataPathsHasNoExternalEffect(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := resolveDataPaths("", "")
	if err != nil {
		t.Fatal(err)
	}
	if paths.sessionRoot != filepath.Join(home, ".easycode", "sessions") ||
		paths.catalogPath != filepath.Join(home, ".easycode", "state.sqlite") {
		t.Fatalf("paths = %#v", paths)
	}
	if _, err := os.Stat(filepath.Join(home, ".easycode")); !os.IsNotExist(err) {
		t.Fatalf("resolving paths created data home: %v", err)
	}
}

func TestResolveDataPathsAllowsIndependentInjection(t *testing.T) {
	sessionRoot := filepath.Join(t.TempDir(), "sessions")
	catalogPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	paths, err := resolveDataPaths(sessionRoot, catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	if paths.sessionRoot != sessionRoot || paths.catalogPath != catalogPath {
		t.Fatalf("paths = %#v", paths)
	}
}
