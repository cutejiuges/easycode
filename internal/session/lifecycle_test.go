package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
)

func TestCreateAndReopenRootJournal(t *testing.T) {
	t.Parallel()
	repository, err := NewRepository(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	beforeCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	creationBase := t.TempDir()
	creationCWD := filepath.Join(creationBase, "nested", "..")
	writer, records, err := CreateRootJournal(
		context.Background(), repository,
		Identity{SessionID: testSessionID, ThreadID: testThreadID},
		RootJournalConfig{
			Provider: domain.ProviderOpenAI, ProviderWire: "responses", Model: "fixture-model",
			CreationCWD: creationCWD, CreatedAt: time.Unix(123, 0).UTC(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Sequence != 1 || records[1].Sequence != 2 || records[0].BatchID != 1 {
		t.Fatalf("initial records = %#v", records)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	afterCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if afterCWD != beforeCWD {
		t.Fatalf("CreateRootJournal changed cwd from %q to %q", beforeCWD, afterCWD)
	}

	lease, err := repository.Open(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewReplayPlanner().Plan(loaded)
	if err != nil {
		t.Fatal(err)
	}
	wantCWD, err := filepath.Abs(creationCWD)
	if err != nil {
		t.Fatal(err)
	}
	if plan.SessionMetadata.CreationCWD != filepath.Clean(wantCWD) {
		t.Fatalf("creation cwd = %q, want %q", plan.SessionMetadata.CreationCWD, filepath.Clean(wantCWD))
	}

	writer, err = StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
	if err != nil {
		t.Fatal(err)
	}
	appended, err := writer.AppendBatch(context.Background(), []RecordDraft{{
		EventKind: EventTurnStarted, TurnID: testTurnID, Payload: TurnStartedPayload{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if appended[0].Sequence != 3 || appended[0].BatchID != 3 {
		t.Fatalf("appended record = %#v", appended[0])
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRootJournalDoesNotPersistConnectionSecrets(t *testing.T) {
	t.Parallel()
	const (
		apiKey     = "sk-fixture-secret"
		baseURL    = "https://secret.example.invalid/v1"
		authHeader = "Bearer fixture-authorization"
		cookie     = "session=fixture-cookie"
		configPath = "/private/config/easycode.json"
	)
	repository, err := NewRepository(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	writer, _, err := CreateRootJournal(
		context.Background(), repository,
		Identity{SessionID: testSessionID, ThreadID: testThreadID},
		RootJournalConfig{
			Provider: domain.ProviderAnthropic, ProviderWire: "messages", Model: "fixture-model",
			CreationCWD: t.TempDir(), CreatedAt: time.Unix(1, 0).UTC(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	path, err := repository.JournalPath(testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, secret := range map[string]string{
		"api key": apiKey, "base URL": baseURL, "authorization": authHeader,
		"cookie": cookie, "config path": configPath,
	} {
		if strings.Contains(string(content), secret) {
			t.Fatalf("journal contains %s", name)
		}
	}
}

func TestNormalizeCreationCWDRejectsEmptyAndDoesNotResolveSymlink(t *testing.T) {
	t.Parallel()
	if _, err := NormalizeCreationCWD(" "); err == nil {
		t.Fatal("NormalizeCreationCWD(empty) unexpectedly succeeded")
	}
	base := t.TempDir()
	value := filepath.Join(base, "missing", "..", "logical")
	got, err := NormalizeCreationCWD(value)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(value) {
		t.Fatalf("NormalizeCreationCWD() = %q, want %q", got, filepath.Clean(value))
	}
}

func TestNewRootIdentityProducesIndependentUUIDv7Values(t *testing.T) {
	t.Parallel()
	identity, err := NewRootIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if !identity.SessionID.Valid() || !identity.ThreadID.Valid() || string(identity.SessionID) == string(identity.ThreadID) {
		t.Fatalf("NewRootIdentity() = %#v", identity)
	}
}
