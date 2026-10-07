//go:build darwin || linux

package projectinstructions

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestLoaderSupportsWorktreeMarkerAndLimitsNoProjectToStartupDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".git"), "gitdir: elsewhere")
	nested := filepath.Join(root, "nested")
	mustMkdir(t, nested)
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "root")
	mustWrite(t, filepath.Join(nested, "AGENTS.md"), "nested")
	if got := len(mustLoad(t, nested, DefaultMaxBytes).Documents()); got != 2 {
		t.Fatalf("worktree documents = %d", got)
	}

	outside := t.TempDir()
	leaf := filepath.Join(outside, "leaf")
	mustMkdir(t, leaf)
	mustWrite(t, filepath.Join(outside, "AGENTS.md"), "must not load")
	mustWrite(t, filepath.Join(leaf, "CLAUDE.md"), "leaf only")
	snapshot := mustLoad(t, leaf, DefaultMaxBytes)
	if len(snapshot.Documents()) != 1 || snapshot.Documents()[0].Source() != "CLAUDE.md" {
		t.Fatalf("no-project documents = %#v", snapshot.Documents())
	}
}

func TestLoaderWithoutProjectDoesNotOpenParentCandidates(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	mustWrite(t, filepath.Join(parent, "target"), "must not read")
	if err := os.Symlink("target", filepath.Join(parent, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	startup := filepath.Join(parent, "startup")
	mustMkdir(t, startup)
	mustWrite(t, filepath.Join(startup, "CLAUDE.md"), "startup only")
	snapshot := mustLoad(t, startup, DefaultMaxBytes)
	if len(snapshot.Documents()) != 1 || snapshot.Documents()[0].Source() != "CLAUDE.md" ||
		snapshot.Documents()[0].Content() != "startup only" {
		t.Fatalf("no-project documents = %#v", snapshot.Documents())
	}
}

func TestLoaderPreservesEmptySelectedFileAndDoesNotUseFallback(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "")
	mustWrite(t, filepath.Join(root, "CLAUDE.md"), "fallback")
	snapshot := mustLoad(t, root, DefaultMaxBytes)
	if len(snapshot.Documents()) != 1 || snapshot.Documents()[0].Source() != "AGENTS.md" || snapshot.Documents()[0].Content() != "" {
		t.Fatalf("documents = %#v", snapshot.Documents())
	}
}

func TestLoaderBudgetTruncatesAndStopsLaterDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	nested := filepath.Join(root, "nested")
	mustMkdir(t, nested)
	mustWrite(t, filepath.Join(root, "AGENTS.md"), strings.Repeat("界", 100))
	mustWrite(t, filepath.Join(nested, "AGENTS.md"), "must not appear")
	full := mustLoad(t, root, DefaultMaxBytes)
	limit := len(full.RenderedText()) - 1
	snapshot := mustLoad(t, nested, limit)
	if !snapshot.Truncated() || len(snapshot.Documents()) != 1 || strings.Contains(snapshot.RenderedText(), "must not appear") {
		t.Fatalf("snapshot = %#v text=%q", snapshot, snapshot.RenderedText())
	}
	if !strings.HasPrefix(strings.Repeat("界", 100), snapshot.Documents()[0].Content()) {
		t.Fatal("root content was not truncated at a valid prefix")
	}
}

func TestLoaderRejectsInvalidByteLimitsBeforeReading(t *testing.T) {
	t.Parallel()
	if _, err := NewLoader(domain.MaxProjectInstructionsBytes); err != nil {
		t.Fatalf("hard limit rejected: %v", err)
	}
	for _, maxBytes := range []int{0, -1, domain.MaxProjectInstructionsBytes + 1, math.MaxInt} {
		if _, err := NewLoader(maxBytes); err == nil {
			t.Fatalf("byte limit %d unexpectedly accepted", maxBytes)
		}
	}

	path := filepath.Join(t.TempDir(), "AGENTS.md")
	mustWrite(t, path, "must remain unread")
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := readInstructionFile(file, math.MaxInt); err == nil {
		t.Fatal("read accepted an excessive byte limit")
	}
	offset, err := file.Seek(0, 1)
	if err != nil || offset != 0 {
		t.Fatalf("invalid limit consumed file bytes: offset=%d err=%v", offset, err)
	}
}

func TestLoaderRevisionIgnoresFileMetadataAndReplacementInode(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	path := filepath.Join(root, "AGENTS.md")
	mustWrite(t, path, "stable")
	first := mustLoad(t, root, DefaultMaxBytes)
	if err := os.Chtimes(path, time.Unix(10, 0), time.Unix(20, 0)); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(root, "replacement")
	mustWrite(t, replacement, "stable")
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	second := mustLoad(t, root, DefaultMaxBytes)
	if first.Revision() != second.Revision() {
		t.Fatalf("metadata changed revision: %s != %s", first.Revision(), second.Revision())
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

func TestLoaderRejectsInvalidUTF8AndReadFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte{0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	loader, _ := NewLoader(DefaultMaxBytes)
	if _, err := loader.Load(root); !errors.Is(err, &fault.Error{Code: fault.CodeProjectInstructionsInvalidUTF8}) {
		t.Fatalf("invalid UTF-8 error = %v", err)
	}
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "content")
	loader.hooks.afterOpenFile = func(_ string, file *os.File) { _ = file.Close() }
	if _, err := loader.Load(root); !errors.Is(err, &fault.Error{Code: fault.CodeProjectInstructionsRead}) {
		t.Fatalf("read failure error = %v", err)
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
