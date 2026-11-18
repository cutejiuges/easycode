package skill

import (
	"context"
	"easycode/internal/extension/plugin"
	"os"
	"path/filepath"
	"testing"
)

func pluginFixture(t *testing.T, kind string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "plugin", "testdata", kind))); err != nil {
		t.Fatal(err)
	}
	return root
}
func pluginFile(t *testing.T, root, relative, text string) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func loadPluginSkills(t *testing.T, project string, roots ...string) (*Local, *plugin.Sources) {
	t.Helper()
	if project == "" {
		project = t.TempDir()
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	source := plugin.NewSources(plugin.Options{})
	t.Cleanup(func() {
		if err := source.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	views, contributions, err := source.Load(context.Background(), project, roots)
	if err != nil {
		t.Fatal(err)
	}
	l := NewLocal()
	if err := l.Load(context.Background(), project, home, contributions, views); err != nil {
		t.Fatal(err)
	}
	return l, source
}
