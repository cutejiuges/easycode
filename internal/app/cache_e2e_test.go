package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/headless"
	"easycode/internal/secret"
	"easycode/internal/session"
)

func TestCachedProvidersSameThreadLiveAndRestoredCanonicalRequests(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			var mu sync.Mutex
			var requests [][]byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				requests = append(requests, body)
				n := len(requests)
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write(activeReasoningFixture(family, n, false))
			}))
			defer server.Close()
			cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("cache-secret")}}, true)
			cfg = reasoningFixtureConfig(t, cfg)
			live, err := openChatResources(context.Background(), cfg, filepath.Join(t.TempDir(), "sessions"), "", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer live.close(context.Background())
			submitAppTurn(t, live, "first")
			submitAppTurn(t, live, "second")
			thread := live.identity.ThreadID
			path, err := live.service.repository.JournalPath(thread)
			if err != nil {
				t.Fatal(err)
			}
			durable, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
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
			if err := os.WriteFile(copyPath, durable, 0600); err != nil {
				t.Fatal(err)
			}
			_ = repo.Close()
			restored, err := openChatResources(context.Background(), cfg, root, string(thread), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer restored.close(context.Background())
			submitAppTurn(t, live, "third")
			submitAppTurn(t, restored, "third")
			mu.Lock()
			captured := append([][]byte(nil), requests...)
			mu.Unlock()
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

func TestInvalidCacheConfigurationHasNoNetworkOrSessionSideEffects(t *testing.T) {
	clearProviderEnvironment(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	for _, field := range []string{`"openai_transport":null`, `"openai_prompt_cache_key":"future"`, `"anthropic_prompt_cache":"ephemeral_5m"`, `"openai_transport":"http_sse","openai_transport":"websocket_auto"`} {
		root := filepath.Join(t.TempDir(), "sessions")
		path := filepath.Join(t.TempDir(), "config.json")
		body := fmt.Sprintf(`{"provider":"openai","base_url":%q,"api_key":"cache-private-secret","model":"gpt-test",%s}`, server.URL, field)
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		result := Run(context.Background(), Options{Mode: headless.ModeText, Prompt: "hello", ConfigPath: path, SessionDataRoot: root, Output: &output})
		if result.Failure.Code != fault.CodeInvalidConfiguration || requests.Load() != 0 {
			t.Fatal("invalid cache config had a side effect")
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("invalid config created session directory")
		}
		if strings.Contains(result.Failure.Message, "cache-private-secret") || strings.Contains(output.String(), "cache-private-secret") {
			t.Fatal("configuration leaked secret")
		}
	}
}
