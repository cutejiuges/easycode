package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/session"
)

func TestHookCacheStableSourcesAndRestoredNativeRequests(t *testing.T) {
	testHookCacheStableSources(t, domain.HookPreToolUse)
}
func TestPostHookCacheStableSourcesAndRestoredNativeRequests(t *testing.T) {
	testHookCacheStableSources(t, domain.HookPostToolUse)
}
func testHookCacheStableSources(t *testing.T, event domain.HookEvent) {
	t.Helper()
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			workspace := t.TempDir()
			if event == domain.HookPostToolUse {
				writePostHookConfig(t, workspace, "printf never-run", domain.HookFeedbackNone)
			} else {
				writeHookConfig(t, workspace, "printf never-run")
			}
			var mu sync.Mutex
			var requests [][]byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				mu.Lock()
				requests = append(requests, b)
				n := len(requests)
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				var calls []writeScenarioCall
				if n == 1 {
					calls = []writeScenarioCall{{"Write", `{"file_path":"marker","content":"must-not-write"}`}}
				}
				serveWriteScenarioSample(w, family, n, calls)
			}))
			defer server.Close()
			cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("hook-cache-private")}}, true)
			root := filepath.Join(t.TempDir(), "sessions")
			live, err := openChatResources(context.Background(), cfg, root, "", workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer live.close(context.Background())
			submitAppTurn(t, live, "seed")
			path, _ := live.service.repository.JournalPath(live.identity.ThreadID)
			durable, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if event == domain.HookPostToolUse {
				writePostHookConfig(t, workspace, "printf changed-config-must-not-affect-prompt", domain.HookFeedbackModel)
			} else {
				writeHookConfig(t, workspace, "printf changed-config-must-not-affect-prompt")
			}
			copied := filepath.Join(t.TempDir(), "sessions")
			repo, err := session.OpenOrCreateRepository(copied)
			if err != nil {
				t.Fatal(err)
			}
			lease, err := repo.Create(context.Background(), live.identity.ThreadID)
			if err != nil {
				t.Fatal(err)
			}
			lease.Close()
			copyPath, _ := repo.JournalPath(live.identity.ThreadID)
			if err := os.WriteFile(copyPath, durable, 0600); err != nil {
				t.Fatal(err)
			}
			repo.Close()
			restored, err := openChatResources(context.Background(), cfg, copied, string(live.identity.ThreadID), workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.close(context.Background())
			lf, err := live.transcriptQueries.projector.(provider.Conversation).HistoryFootprint()
			if err != nil {
				t.Fatal(err)
			}
			rf, err := restored.transcriptQueries.projector.(provider.Conversation).HistoryFootprint()
			if err != nil || !reflect.DeepEqual(lf, rf) {
				t.Fatal("hook history footprint changed on restore", err)
			}
			submitAppTurn(t, live, "next")
			submitAppTurn(t, restored, "next")
			mu.Lock()
			captured := append([][]byte(nil), requests...)
			mu.Unlock()
			if len(captured) != 4 || !bytes.Equal(captured[2], captured[3]) {
				t.Fatal("hook config or restore changed canonical request")
			}
			assertRequestFingerprintEqual(t, captured[2], captured[3])
			for _, forbidden := range []string{"hook-cache-private", "changed-config-must-not-affect-prompt", workspace} {
				if bytes.Contains(captured[2], []byte(forbidden)) {
					t.Fatal("hook source polluted stable prefix", forbidden)
				}
			}
			if !bytes.Contains(captured[2], []byte("hook_trust_required")) {
				t.Fatal("hook denial missing from history")
			}
			if family == domain.ProviderOpenAI && !bytes.Contains(captured[2], []byte(`"prompt_cache_key":"easycode:`)) {
				t.Fatal("missing thread cache key")
			}
			if family == domain.ProviderAnthropic && (!bytes.Contains(captured[2], []byte(`"ttl":"1h"`)) || bytes.Count(captured[2], []byte(`"cache_control"`)) > 4) {
				t.Fatal("invalid Anthropic cache marker")
			}
			if _, err := os.Stat(filepath.Join(workspace, "marker")); !os.IsNotExist(err) {
				t.Fatal("headless hook or tool ran")
			}
		})
	}
}
