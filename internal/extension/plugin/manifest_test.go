package plugin

import (
	"context"
	"easycode/internal/extension"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, kind string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("testdata", kind))); err != nil {
		t.Fatal(err)
	}
	return root
}

func load(t *testing.T, options Options, dirs ...string) (*Sources, []extension.PluginSource, []extension.Contribution) {
	t.Helper()
	sources := NewSources(options)
	t.Cleanup(func() {
		if err := sources.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	cwd, _ := os.Getwd()
	views, contributions, err := sources.Load(context.Background(), cwd, dirs)
	if err != nil {
		t.Fatal(err)
	}
	return sources, views, contributions
}

func TestExplicitLoadCancellationAndSingleLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	root := fixture(t, "claude")
	s := NewSources(Options{BeforeOpen: func(value string) {
		if value == ".claude-plugin/plugin.json" {
			cancel()
		}
	}})
	cwd, _ := os.Getwd()
	views, cs, err := s.Load(ctx, cwd, []string{root})
	if err != context.Canceled || views != nil || cs != nil {
		t.Fatalf("cancelled load: %v", err)
	}
	if len(s.roots) != 0 || len(s.active) != 0 {
		t.Fatal("cancelled load retained resources")
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Load(context.Background(), cwd, []string{root}); err == nil {
		t.Fatal("reloaded closed source")
	}
	for _, dirs := range [][]string{{""}, {strings.Repeat("x", 4097)}, {string([]byte{0xff})}, make([]string, 33)} {
		if ValidateDirectories(dirs) == nil {
			t.Fatal("invalid arguments")
		}
	}
}
