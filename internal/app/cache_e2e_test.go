package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/secret"
	"easycode/internal/session"
)

func TestCachedProvidersSameThreadLiveAndRestoredCanonicalRequests(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			var recorder requestRecorder
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestIndex, _ := recorder.record(t, r)
				n := requestIndex + 1
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write(activeReasoningFixture(family, n, false))
			}))
			defer server.Close()
			cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("cache-secret")}}, true)
			cfg = reasoningFixtureConfig(t, cfg)
			live := mustOpenChatResources(t, context.Background(), cfg, filepath.Join(t.TempDir(), "sessions"), "", t.TempDir())
			defer live.close(context.Background())
			submitAppTurn(t, live, "first")
			submitAppTurn(t, live, "second")
			thread := live.identity.ThreadID
			path, err := live.service.repository.JournalPath(thread)
			if err != nil {
				t.Fatal(err)
			}
			durable := readAppTestFile(t, path)
			root := filepath.Join(t.TempDir(), "sessions")
			repo, err := session.OpenOrCreateRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			lease, err := repo.Create(context.Background(), thread)
			if err != nil {
				t.Fatal(err)
			}
			_ = lease.Close()
			copyPath, _ := repo.JournalPath(thread)
			writeAppTestFile(t, copyPath, durable, 0600)
			_ = repo.Close()
			restored := mustOpenChatResources(t, context.Background(), cfg, root, string(thread), t.TempDir())
			defer restored.close(context.Background())
			submitAppTurn(t, live, "third")
			submitAppTurn(t, restored, "third")
			captured := recorder.snapshot()
			if len(captured) != 4 || !bytes.Equal(captured[2], captured[3]) {
				t.Fatal("enabled cache full request bytes differ between uninterrupted and restored same thread")
			}
			if bytes.Contains(captured[2], []byte("cache-secret")) {
				t.Fatal("request leaked secret")
			}
			if family == domain.ProviderOpenAI && !bytes.Contains(captured[2], []byte(`"prompt_cache_key":"easycode:`)) {
				t.Fatal("thread key omitted")
			}
			if family == domain.ProviderAnthropic && (!bytes.Contains(captured[2], []byte(`"ttl":"1h"`)) || bytes.Count(captured[2], []byte(`"cache_control"`)) > 4) {
				t.Fatal("cache markers invalid")
			}
			if !bytes.Contains(captured[2], []byte("answer-1")) || !bytes.Contains(captured[2], []byte("answer-2")) {
				t.Fatal("native output order lost")
			}
		})
	}
}
