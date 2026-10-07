package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/secret"
)

func TestProjectInstructionDiscoveryFailsBeforeApplicationResourcesOpen(t *testing.T) {
	t.Parallel()
	startupDirectory := t.TempDir()
	if err := os.Mkdir(filepath.Join(startupDirectory, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "private-project-instructions")
	if err := os.WriteFile(target, []byte("project-secret-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(startupDirectory, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	applicationConfig := config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, BaseURL: server.URL,
		APIKey: secret.New("test-key"), Model: "gpt-test",
	}}
	stateRoot := t.TempDir()
	paths := dataPaths{
		sessionRoot: filepath.Join(stateRoot, "sessions"),
		catalogPath: filepath.Join(stateRoot, "state.sqlite"),
	}
	resources, err := openChatResourcesWithSelection(
		context.Background(), applicationConfig, paths, "", true, startupDirectory,
	)
	if resources != nil || !errors.Is(err, &fault.Error{Code: fault.CodeProjectInstructionsUnsafe}) {
		t.Fatalf("resources=%#v error=%v", resources, err)
	}
	for _, sensitive := range []string{startupDirectory, target, "project-secret-content"} {
		if bytes.Contains([]byte(err.Error()), []byte(sensitive)) {
			t.Fatalf("discovery error leaked %q: %v", sensitive, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("discovery failure made %d provider requests", requests.Load())
	}
	for _, path := range []string{paths.sessionRoot, paths.catalogPath} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("discovery failure created %q: %v", path, statErr)
		}
	}
}

func TestProjectInstructionsAreStartupScopedAndNeverJournaled(t *testing.T) {
	t.Parallel()
	const firstMarker = "startup-snapshot-first-marker"
	const secondMarker = "startup-snapshot-second-marker"
	startupDirectory := t.TempDir()
	if err := os.Mkdir(filepath.Join(startupDirectory, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	instructionPath := filepath.Join(startupDirectory, "AGENTS.md")
	if err := os.WriteFile(instructionPath, []byte(firstMarker), 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var requests [][]byte
	var turn atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		mu.Lock()
		requests = append(requests, append([]byte(nil), body...))
		mu.Unlock()
		writer.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIAppTurn(writer, int(turn.Add(1)))
	}))
	defer server.Close()
	applicationConfig := config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, BaseURL: server.URL,
		APIKey: secret.New("test-key"), Model: "gpt-test",
	}}
	firstDataRoot := filepath.Join(t.TempDir(), "sessions")
	first, err := openChatResources(
		context.Background(), applicationConfig, firstDataRoot, "", startupDirectory,
	)
	if err != nil {
		t.Fatal(err)
	}
	submitAppTurn(t, first, "first")
	if err := os.WriteFile(instructionPath, []byte(secondMarker), 0o600); err != nil {
		t.Fatal(err)
	}
	submitAppTurn(t, first, "second")
	threadID := first.identity.ThreadID
	if err := first.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	journalPath, journalBeforeResume := readProjectInstructionJournal(t, firstDataRoot)

	second, err := openChatResources(
		context.Background(), applicationConfig, firstDataRoot, string(threadID), startupDirectory,
	)
	if err != nil {
		t.Fatal(err)
	}
	journalAfterResume, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(journalAfterResume, journalBeforeResume) {
		t.Fatal("resume with changed project instructions rewrote existing journal bytes")
	}
	if second.identity.ThreadID != threadID || len(second.history.Turns) != 2 {
		t.Fatalf("changed project instructions changed restored session state: %#v %#v", second.identity, second.history)
	}
	submitAppTurn(t, second, "third")
	if err := second.close(context.Background()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	captured := append([][]byte(nil), requests...)
	mu.Unlock()
	if len(captured) != 3 {
		t.Fatalf("request count = %d", len(captured))
	}
	for index := 0; index < 2; index++ {
		if !bytes.Contains(captured[index], []byte(firstMarker)) || bytes.Contains(captured[index], []byte(secondMarker)) {
			t.Fatalf("active process reloaded project instructions in request %d: %s", index, captured[index])
		}
	}
	if !bytes.Contains(captured[2], []byte(secondMarker)) || bytes.Contains(captured[2], []byte(firstMarker)) {
		t.Fatalf("new process did not reload project instructions: %s", captured[2])
	}
	if bytes.Count(captured[2], []byte(secondMarker)) != 1 {
		t.Fatalf("resumed request does not contain exactly one current project context: %s", captured[2])
	}
	for index, request := range captured {
		if bytes.Contains(request, []byte(startupDirectory)) {
			t.Fatalf("request %d contains absolute startup directory: %s", index, request)
		}
	}
	if err := filepath.WalkDir(firstDataRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(content, []byte(firstMarker)) || bytes.Contains(content, []byte(secondMarker)) {
			t.Fatalf("session artifact contains project instructions: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestResumeUsesEquivalentSnapshotFromCurrentAbsoluteCWD(t *testing.T) {
	t.Parallel()
	const rootMarker = "portable-root-project-marker"
	const nestedMarker = "portable-nested-project-marker"
	firstCWD := makeEquivalentProjectTree(t, rootMarker, nestedMarker)
	secondCWD := makeEquivalentProjectTree(t, rootMarker, nestedMarker)

	var mu sync.Mutex
	var requests [][]byte
	logicalTurns := []int{1, 2, 1, 2}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		mu.Lock()
		index := len(requests)
		requests = append(requests, append([]byte(nil), body...))
		mu.Unlock()
		writer.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIAppTurn(writer, logicalTurns[index])
	}))
	defer server.Close()
	applicationConfig := config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, BaseURL: server.URL,
		APIKey: secret.New("test-key"), Model: "gpt-test",
	}}
	dataRoot := filepath.Join(t.TempDir(), "sessions")
	created, err := openChatResources(context.Background(), applicationConfig, dataRoot, "", firstCWD)
	if err != nil {
		t.Fatal(err)
	}
	submitAppTurn(t, created, "first")
	threadID := created.identity.ThreadID
	if err := created.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	processCWDBefore, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := openChatResources(context.Background(), applicationConfig, dataRoot, string(threadID), secondCWD)
	if err != nil {
		t.Fatal(err)
	}
	submitAppTurn(t, resumed, "second")
	if err := resumed.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	processCWDAfter, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if processCWDAfter != processCWDBefore {
		t.Fatalf("resume changed process cwd from %q to %q", processCWDBefore, processCWDAfter)
	}

	control, err := openChatResources(
		context.Background(), applicationConfig, filepath.Join(t.TempDir(), "sessions"), "", firstCWD,
	)
	if err != nil {
		t.Fatal(err)
	}
	submitAppTurn(t, control, "first")
	submitAppTurn(t, control, "second")
	if err := control.close(context.Background()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	captured := append([][]byte(nil), requests...)
	mu.Unlock()
	if len(captured) != 4 || !bytes.Equal(captured[1], captured[3]) {
		t.Fatalf("equivalent snapshots from different absolute cwd changed restored request: %#v", captured)
	}
	for index, request := range captured {
		for _, absolute := range []string{firstCWD, secondCWD, filepath.Dir(firstCWD), filepath.Dir(secondCWD)} {
			if bytes.Contains(request, []byte(absolute)) {
				t.Fatalf("request %d contains absolute project path %q: %s", index, absolute, request)
			}
		}
	}
}

func makeEquivalentProjectTree(t *testing.T, rootMarker string, nestedMarker string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(rootMarker), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "AGENTS.md"), []byte(nestedMarker), 0o600); err != nil {
		t.Fatal(err)
	}
	return nested
}

func readProjectInstructionJournal(t *testing.T, root string) (string, []byte) {
	t.Helper()
	var journalPath string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Ext(path) == ".jsonl" {
			if journalPath != "" {
				t.Fatalf("multiple journals found beneath %s", root)
			}
			journalPath = path
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if journalPath == "" {
		t.Fatalf("journal not found beneath %s", root)
	}
	content, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	return journalPath, content
}
