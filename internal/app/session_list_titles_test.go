//go:build darwin || linux

package app

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/session"
)

func TestSessionListShowsNamedAndUnnamedSessionsOffline(t *testing.T) {
	ctx := context.Background()
	dir := privateAppTempDir(t)
	root := filepath.Join(dir, "sessions")
	repository, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	title := domain.SessionTitle(`解析 👩‍💻 \parser`)
	const model = "fixture\x1b[31m\nmodel"
	threads := make([]domain.ThreadID, 0, 3)
	for _, name := range []domain.SessionTitle{title, title, ""} {
		identity, err := session.GenerateRootIdentity()
		if err != nil {
			t.Fatal(err)
		}
		writer, _, err := session.CreateRootJournal(ctx, repository, identity, session.RootJournalConfig{
			Provider: domain.ProviderOpenAI, ProviderWire: "responses", Model: model,
			CreationCWD: dir,
		})
		if err != nil {
			t.Fatal(err)
		}
		finishSessionTitleFixture(t, writer, name)
		threads = append(threads, identity.ThreadID)
	}
	paths := dataPaths{sessionRoot: root, catalogPath: filepath.Join(dir, "state.sqlite")}
	var stdout, stderr bytes.Buffer
	outcome := ListSessions(ctx, SessionListOptions{
		All: true, Limit: 3, SessionDataRoot: paths.sessionRoot, SessionCatalogPath: paths.catalogPath,
		Output: &stdout, ErrorOutput: &stderr,
	})
	if outcome.Class != ExitSuccess || stderr.Len() != 0 {
		t.Fatalf("offline list = %+v, %q", outcome, &stderr)
	}
	output := stdout.String()
	if strings.Count(output, "  Title: "+string(title)+"\n") != 2 || strings.Count(output, "Title: ") != 2 ||
		strings.Contains(output, "\x1b") || !strings.Contains(output, "easycode --resume <thread-id>") {
		t.Fatalf("unexpected list output: %s", output)
	}
	for _, thread := range threads {
		if strings.Count(output, "\n"+string(thread)+"\n") != 1 {
			t.Fatalf("list did not preserve complete thread id %q: %s", thread, output)
		}
	}
	applicationConfig := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: model}}
	before, err := selectContinueThread(ctx, paths, repository, applicationConfig, "responses", dir)
	if err != nil || before != threads[2] {
		t.Fatalf("--continue before rename chose %q, %v", before, err)
	}
	lease, err := repository.Open(ctx, threads[0])
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := session.NewLoader().Load(ctx, lease)
	if err != nil {
		_ = lease.Close()
		t.Fatal(err)
	}
	writer, err := session.StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
	if err != nil {
		_ = lease.Close()
		t.Fatal(err)
	}
	finishSessionTitleFixture(t, writer, "Revisited parser")
	after, err := selectContinueThread(ctx, paths, repository, applicationConfig, "responses", dir)
	if err != nil || after != threads[0] {
		t.Fatalf("--continue after rename chose %q, %v", after, err)
	}
}

func finishSessionTitleFixture(t *testing.T, writer *session.JournalWriter, title domain.SessionTitle) {
	t.Helper()
	ctx := context.Background()
	var appendErr error
	if title != "" {
		var draft session.RecordDraft
		draft, appendErr = session.NewSessionTitleUpdatedDraft(title)
		if appendErr == nil {
			_, appendErr = writer.AppendBatch(ctx, []session.RecordDraft{draft})
		}
	}
	if closeErr := writer.Close(ctx); appendErr != nil || closeErr != nil {
		t.Fatalf("write title: %v; close: %v", appendErr, closeErr)
	}
}
