package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/headless"
	"easycode/internal/secret"
	"easycode/internal/session"
)

func TestRunContinueReportsNotFoundWithoutProviderRequestOrReplacement(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	directory := privateAppTempDir(t)
	root := filepath.Join(directory, "sessions")
	catalogPath := filepath.Join(directory, "state.sqlite")
	outcome := Run(context.Background(), Options{
		Mode: headless.ModeText, Prompt: "hello", ContinueSession: true,
		ConfigPath:      writeAppConfig(t, t.TempDir(), domain.ProviderOpenAI, server.URL),
		SessionDataRoot: root, SessionCatalogPath: catalogPath, Output: &bytes.Buffer{},
	})
	if outcome.Failure.Code != fault.CodeSessionNotFound || requests.Load() != 0 {
		t.Fatalf("outcome=%#v requests=%d", outcome, requests.Load())
	}
	if countJournalFiles(t, root) != 0 {
		t.Fatal("continue without a candidate created a replacement journal")
	}
}

func TestRunJSONContinueReportsOriginalIdentityAsResumed(t *testing.T) {
	var turn atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIAppTurn(writer, int(turn.Add(1)))
	}))
	defer server.Close()
	directory := privateAppTempDir(t)
	root := filepath.Join(directory, "sessions")
	catalogPath := filepath.Join(directory, "state.sqlite")
	configPath := writeAppConfig(t, t.TempDir(), domain.ProviderOpenAI, server.URL)
	var firstOutput bytes.Buffer
	first := Run(context.Background(), Options{
		Mode: headless.ModeJSON, Prompt: "first", ConfigPath: configPath,
		SessionDataRoot: root, SessionCatalogPath: catalogPath, Output: &firstOutput,
	})
	if first.ExitCode() != 0 {
		t.Fatalf("first outcome = %#v", first)
	}
	started := decodeThreadStarted(t, firstOutput.String())
	var continuedOutput bytes.Buffer
	continued := Run(context.Background(), Options{
		Mode: headless.ModeJSON, Prompt: "second", ConfigPath: configPath, ContinueSession: true,
		SessionDataRoot: root, SessionCatalogPath: catalogPath, Output: &continuedOutput,
	})
	if continued.ExitCode() != 0 {
		t.Fatalf("continued outcome = %#v", continued)
	}
	continuedStarted := decodeThreadStarted(t, continuedOutput.String())
	if !continuedStarted.Resumed || continuedStarted.SessionID != started.SessionID || continuedStarted.ThreadID != started.ThreadID {
		t.Fatalf("continued start=%#v first=%#v", continuedStarted, started)
	}
	if strings.Contains(continuedOutput.String(), "answer-1") || !strings.Contains(continuedOutput.String(), "answer-2") {
		t.Fatalf("continued output replayed history: %s", continuedOutput.String())
	}
}

func TestRunContinueRejectsUnindexedBusyJournal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIAppTurn(writer, 1)
	}))
	defer server.Close()
	directory := privateAppTempDir(t)
	root := filepath.Join(directory, "sessions")
	catalogPath := filepath.Join(directory, "state.sqlite")
	providerConfig := config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, BaseURL: server.URL,
		APIKey: secret.New("continue-secret"), Model: "gpt-test",
	}}
	active, err := openChatResources(context.Background(), providerConfig, root, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = active.close(context.Background()) }()
	outcome := Run(context.Background(), Options{
		Mode: headless.ModeText, Prompt: "hello", ContinueSession: true,
		ConfigPath:      writeAppConfig(t, t.TempDir(), domain.ProviderOpenAI, server.URL),
		SessionDataRoot: root, SessionCatalogPath: catalogPath, Output: &bytes.Buffer{},
	})
	if outcome.Failure.Code != fault.CodeSessionBusy {
		t.Fatalf("busy outcome = %#v", outcome)
	}
}

func TestContinueSelectionMatrixUsesExactCompatibilityAndStableTieBreak(t *testing.T) {
	directory := privateAppTempDir(t)
	paths := dataPaths{
		sessionRoot: filepath.Join(directory, "sessions"),
		catalogPath: filepath.Join(directory, "state.sqlite"),
	}
	repository, err := session.OpenOrCreateRepository(paths.sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	currentCWD := filepath.Join(directory, "project-a")
	otherCWD := filepath.Join(directory, "project-b")
	for _, value := range []string{currentCWD, otherCWD} {
		if err := os.MkdirAll(value, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	baseTime := time.Date(2026, time.September, 30, 10, 0, 0, 0, time.UTC)
	writeContinueMatrixJournal(t, repository, currentCWD, domain.ProviderOpenAI, "responses", "gpt-test", baseTime)
	firstTie := writeContinueMatrixJournal(t, repository, currentCWD, domain.ProviderOpenAI, "responses", "gpt-test", baseTime.Add(time.Minute))
	secondTie := writeContinueMatrixJournal(t, repository, currentCWD, domain.ProviderOpenAI, "responses", "gpt-test", baseTime.Add(time.Minute))
	writeContinueMatrixJournal(t, repository, otherCWD, domain.ProviderOpenAI, "responses", "gpt-test", baseTime.Add(2*time.Minute))
	writeContinueMatrixJournal(t, repository, currentCWD, domain.ProviderAnthropic, "messages", "claude-test", baseTime.Add(3*time.Minute))
	writeContinueMatrixJournal(t, repository, currentCWD, domain.ProviderOpenAI, "legacy-responses", "gpt-test", baseTime.Add(4*time.Minute))
	writeContinueMatrixJournal(t, repository, currentCWD, domain.ProviderOpenAI, "responses", "other-model", baseTime.Add(5*time.Minute))
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("selection matrix unexpectedly called Provider")
	}))
	defer server.Close()
	providerConfig := config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, BaseURL: server.URL,
		APIKey: secret.New("matrix-secret"), Model: "gpt-test",
	}}
	service, err := openSessionService(paths.sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	want := firstTie.ThreadID
	if string(secondTie.ThreadID) > string(want) {
		want = secondTie.ThreadID
	}
	selected, err := selectContinueThread(
		context.Background(), paths, service.repository, providerConfig, "responses", currentCWD,
	)
	if err != nil {
		t.Fatal(err)
	}
	if selected != want {
		t.Fatalf("selected thread = %s, want stable tie winner %s", selected, want)
	}
	if err := service.close(); err != nil {
		t.Fatal(err)
	}

	fresh, err := openChatResources(context.Background(), providerConfig, paths.sessionRoot, "", currentCWD)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.identity.ThreadID == selected || len(fresh.history.Turns) != 0 || fresh.resumed {
		t.Fatalf("ordinary startup reused selected session: %#v", fresh)
	}
	if err := fresh.close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestContinueSelectionIsRevalidatedWithoutFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIAppTurn(writer, 1)
	}))
	defer server.Close()
	providerConfig := config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, BaseURL: server.URL,
		APIKey: secret.New("continue-secret"), Model: "gpt-test",
	}}
	for _, test := range []struct {
		name     string
		wantCode fault.Code
		mutate   func(*testing.T, *session.Repository, domain.ThreadID) func()
	}{
		{name: "busy", wantCode: fault.CodeSessionBusy, mutate: occupySelectedJournal},
		{name: "corrupt", wantCode: fault.CodeSessionCorruption, mutate: corruptSelectedJournal},
		{name: "incompatible", wantCode: fault.CodeSessionIncompatible, mutate: replaceWithIncompatibleJournal},
		{name: "symlink replacement", wantCode: fault.CodeSessionCorruption, mutate: replaceWithSymlinkedJournal},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := privateAppTempDir(t)
			paths := dataPaths{
				sessionRoot: filepath.Join(directory, "sessions"),
				catalogPath: filepath.Join(directory, "state.sqlite"),
			}
			cwd := t.TempDir()
			for range 2 {
				resources, err := openChatResources(context.Background(), providerConfig, paths.sessionRoot, "", cwd)
				if err != nil {
					t.Fatal(err)
				}
				if err := resources.close(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			repository, err := session.OpenOrCreateRepository(paths.sessionRoot)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = repository.Close() }()
			var cleanup func()
			resources, err := openChatResourcesWithHooks(
				context.Background(), providerConfig, paths, "", true, cwd,
				chatResourceHooks{afterContinueSelection: func(threadID domain.ThreadID) {
					cleanup = test.mutate(t, repository, threadID)
				}},
			)
			if cleanup != nil {
				cleanup()
			}
			if resources != nil || !errors.Is(err, &fault.Error{Code: test.wantCode}) {
				t.Fatalf("resources=%#v error=%v", resources, err)
			}
		})
	}
}

func TestRunContinueCatalogErrorIsSanitized(t *testing.T) {
	directory := privateAppTempDir(t)
	target := filepath.Join(t.TempDir(), "private-secret-target")
	if err := os.WriteFile(target, []byte("opaque-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(directory, "state.sqlite")
	if err := os.Symlink(target, catalogPath); err != nil {
		t.Fatal(err)
	}
	outcome := Run(context.Background(), Options{
		Mode: headless.ModeText, Prompt: "prompt-secret", ContinueSession: true,
		ConfigPath:      writeAppConfig(t, t.TempDir(), domain.ProviderOpenAI, "https://example.invalid/v1"),
		SessionDataRoot: filepath.Join(directory, "sessions"), SessionCatalogPath: catalogPath,
		Output: &bytes.Buffer{},
	})
	if outcome.Failure.Code != fault.CodeSessionCatalog ||
		strings.Contains(outcome.Failure.Message, directory) || strings.Contains(outcome.Failure.Message, "secret") {
		t.Fatalf("unsafe catalog outcome = %#v", outcome)
	}
}

func occupySelectedJournal(t *testing.T, repository *session.Repository, threadID domain.ThreadID) func() {
	t.Helper()
	lease, err := repository.Open(context.Background(), threadID)
	if err != nil {
		t.Fatal(err)
	}
	return func() { _ = lease.Close() }
}

func corruptSelectedJournal(t *testing.T, repository *session.Repository, threadID domain.ThreadID) func() {
	t.Helper()
	path, err := repository.JournalPath(threadID)
	if err != nil {
		t.Fatal(err)
	}
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
	return func() {}
}

func replaceWithIncompatibleJournal(t *testing.T, repository *session.Repository, threadID domain.ThreadID) func() {
	t.Helper()
	path, err := repository.JournalPath(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	sessionID, err := domain.GenerateSessionID()
	if err != nil {
		t.Fatal(err)
	}
	writer, _, err := session.CreateRootJournal(context.Background(), repository, session.Identity{
		SessionID: sessionID, ThreadID: threadID,
	}, session.RootJournalConfig{
		Provider: domain.ProviderAnthropic, ProviderWire: "messages", Model: "claude-test",
		CreationCWD: t.TempDir(), CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	return func() {}
}

func replaceWithSymlinkedJournal(t *testing.T, repository *session.Repository, threadID domain.ThreadID) func() {
	t.Helper()
	path, err := repository.JournalPath(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(target, []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	return func() {
		content, readErr := os.ReadFile(target)
		if readErr != nil || string(content) != "outside-secret" {
			t.Fatalf("outside target changed: %q, %v", content, readErr)
		}
	}
}

func countJournalFiles(t *testing.T, root string) int {
	t.Helper()
	count := 0
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry != nil && !entry.IsDir() && strings.HasSuffix(path, ".jsonl") {
			count++
		}
		return nil
	})
	return count
}

func writeContinueMatrixJournal(
	t *testing.T,
	repository *session.Repository,
	cwd string,
	family domain.ProviderFamily,
	wire string,
	model string,
	updatedAt time.Time,
) session.Identity {
	t.Helper()
	identity, err := session.GenerateRootIdentity()
	if err != nil {
		t.Fatal(err)
	}
	lease, err := repository.Create(context.Background(), identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	sessionDraft, err := session.NewSessionMetaDraft(session.SessionMetaPayload{
		RootThreadID: identity.ThreadID, CreatedAt: updatedAt.Add(-time.Hour),
		Provider: family, ProviderWire: wire, Model: model,
		CreationCWD: cwd,
	})
	if err != nil {
		t.Fatal(err)
	}
	threadDraft, err := session.NewThreadMetaDraft(session.ThreadMetaPayload{Root: true})
	if err != nil {
		t.Fatal(err)
	}
	var content bytes.Buffer
	for index, draft := range []session.RecordDraft{sessionDraft, threadDraft} {
		record, err := session.BuildRecord(
			identity, draft, uint64(index+1), updatedAt, 1, uint32(index), 2,
		)
		if err != nil {
			t.Fatal(err)
		}
		_, encoded, err := session.EncodeRecord(record)
		if err != nil {
			t.Fatal(err)
		}
		content.Write(encoded)
		content.WriteByte('\n')
	}
	journalPath, err := repository.JournalPath(identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, content.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return identity
}
