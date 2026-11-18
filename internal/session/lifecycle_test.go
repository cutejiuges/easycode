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

func TestRootJournalDoesNotPersistConnectionSecrets(t *testing.T) {
	t.Parallel()
	const (
		apiKey     = "sk-fixture-secret"
		baseURL    = "https://secret.example.invalid/v1"
		authHeader = "Bearer fixture-authorization"
		cookie     = "session=fixture-cookie"
		configPath = "/private/config/easycode.json"
	)
	repository, err := OpenOrCreateRepository(filepath.Join(t.TempDir(), "sessions"))
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
