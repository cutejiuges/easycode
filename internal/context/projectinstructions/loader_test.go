//go:build darwin || linux

package projectinstructions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/fault"

	"golang.org/x/sys/unix"
)

func TestLoaderDiscoversRootToStartupWithFixedPrecedence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	nested := filepath.Join(root, "one", "two")
	mustMkdir(t, nested)
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "root agents")
	mustWrite(t, filepath.Join(root, "CLAUDE.md"), "ignored root claude")
	mustWrite(t, filepath.Join(root, "one", "CLAUDE.md"), "one claude")
	mustWrite(t, filepath.Join(nested, "AGENTS.md"), "nested agents")

	snapshot := mustLoad(t, nested, DefaultMaxBytes)
	wantSources := []string{"AGENTS.md", "one/CLAUDE.md", "one/two/AGENTS.md"}
	if len(snapshot.Documents()) != len(wantSources) {
		t.Fatalf("documents = %#v", snapshot.Documents())
	}
	for index, source := range wantSources {
		if snapshot.Documents()[index].Source() != source {
			t.Fatalf("source %d = %q, want %q", index, snapshot.Documents()[index].Source(), source)
		}
	}
	if strings.Contains(snapshot.RenderedText(), root) || strings.Contains(snapshot.RenderedText(), "ignored root claude") {
		t.Fatalf("rendered text leaked absolute or fallback content: %q", snapshot.RenderedText())
	}
}

func TestLoaderRejectsUnsafeCandidatesAndBoundary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{name: "instruction symlink", setup: func(t *testing.T, root string) {
			mustMkdir(t, filepath.Join(root, ".git"))
			mustWrite(t, filepath.Join(root, "target"), "secret")
			if err := os.Symlink("target", filepath.Join(root, "AGENTS.md")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "instruction directory", setup: func(t *testing.T, root string) {
			mustMkdir(t, filepath.Join(root, ".git"))
			mustMkdir(t, filepath.Join(root, "AGENTS.md"))
		}},
		{name: "instruction fifo", setup: func(t *testing.T, root string) {
			mustMkdir(t, filepath.Join(root, ".git"))
			if err := unix.Mkfifo(filepath.Join(root, "AGENTS.md"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "git symlink", setup: func(t *testing.T, root string) {
			mustMkdir(t, filepath.Join(root, "real-git"))
			if err := os.Symlink("real-git", filepath.Join(root, ".git")); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			test.setup(t, root)
			loader, _ := NewLoader(DefaultMaxBytes)
			_, err := loader.Load(root)
			if !errors.Is(err, &fault.Error{Code: fault.CodeProjectInstructionsUnsafe}) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestLoaderValidatesUTF8BeyondRetainedBudget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	content := append([]byte(strings.Repeat("a", DefaultMaxBytes+10)), 0xff)
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	loader, _ := NewLoader(DefaultMaxBytes)
	if _, err := loader.Load(root); !errors.Is(err, &fault.Error{Code: fault.CodeProjectInstructionsInvalidUTF8}) {
		t.Fatalf("invalid UTF-8 after retained budget error = %v", err)
	}
}

func TestLoaderRejectsCandidateReplacementBetweenInspectionAndOpen(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	candidate := filepath.Join(root, "AGENTS.md")
	mustWrite(t, candidate, "original")
	mustWrite(t, filepath.Join(root, "replacement"), "replacement")
	loader, _ := NewLoader(DefaultMaxBytes)
	replaced := false
	loader.hooks.beforeOpenName = func(name string) {
		if name != "AGENTS.md" || replaced {
			return
		}
		replaced = true
		if err := os.Remove(candidate); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("replacement", candidate); err != nil {
			t.Fatal(err)
		}
	}
	_, err := loader.Load(root)
	if !errors.Is(err, &fault.Error{Code: fault.CodeProjectInstructionsUnsafe}) {
		t.Fatalf("replacement error = %v", err)
	}
}

func mustLoad(t *testing.T, directory string, maxBytes int) domain.ProjectInstructionsSnapshot {
	t.Helper()
	loader, err := NewLoader(maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := loader.Load(filepath.Clean(directory))
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
