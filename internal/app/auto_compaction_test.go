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
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/headless"
	"easycode/internal/secret"
)

func TestAutomaticCompactionRealProvidersAndResume(t *testing.T) {
	runCacheAutomaticCompactionRealProvidersAndResume(t, false)
}
func TestCachedAutomaticCompactionRealProvidersAndResume(t *testing.T) {
	runCacheAutomaticCompactionRealProvidersAndResume(t, true)
}
func runCacheAutomaticCompactionRealProvidersAndResume(t *testing.T, cacheEnabled bool) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, failure := range []bool{false, true} {
			name := string(family)
			if failure {
				name += "/recoverable_failure"
			}
			t.Run(name, func(t *testing.T) {
				var mu sync.Mutex
				var requests [][]byte
				summaries := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					mu.Lock()
					requests = append(requests, body)
					n := len(requests)
					mu.Unlock()
					isSummary := bytes.Contains(body, []byte(`"tool_choice":"none"`)) || bytes.Contains(body, []byte(`"tool_choice":{"type":"none"}`))
					if isSummary {
						mu.Lock()
						summaries++
						mu.Unlock()
						if n == 2 && bytes.Contains(body, []byte("new-exact-correction")) {
							t.Error("pending input entered summary")
						}
						if !bytes.Contains(body, []byte("latest-prior-correction")) {
							t.Error("prior correction omitted")
						}
						if failure {
							http.Error(w, "private-error-body", http.StatusBadRequest)
							return
						}
					}
					w.Header().Set("Content-Type", "text/event-stream")
					if family == domain.ProviderOpenAI {
						writeOpenAIAppTurn(w, n)
					} else {
						var response bytes.Buffer
						writeAnthropicAppTurn(&response, n)
						data := bytes.Replace(response.Bytes(), []byte("data: {\"type\":\"message_stop\"}"), []byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}"), 1)
						_, _ = w.Write(data)
					}
				}))
				defer server.Close()
				budget, _ := contextplan.NewBudget(20000, 0, 0)
				cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, Model: "claude-test", BaseURL: server.URL, APIKey: secret.New("private-test-key")}, ContextBudget: budget}, cacheEnabled)
				root, workspace := filepath.Join(t.TempDir(), "sessions"), t.TempDir()
				resources, err := openChatResources(context.Background(), cfg, root, "", workspace)
				if err != nil {
					t.Fatal(err)
				}
				first := "latest-prior-correction " + strings.Repeat("historical work ", 3500)
				submitAppTurn(t, resources, first)
				submitAppTurn(t, resources, "new-exact-correction\n  keep this indentation  ")
				thread := resources.identity.ThreadID
				if err := resources.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				mu.Lock()
				count, summaryCount := len(requests), summaries
				captured := append([][]byte(nil), requests...)
				mu.Unlock()
				if count != 3 || summaryCount != 1 {
					t.Fatalf("requests=%d summaries=%d", count, summaryCount)
				}
				if !bytes.Contains(captured[2], []byte("new-exact-correction\\n  keep this indentation  ")) {
					t.Fatal("input text lost")
				}
				if !failure && (bytes.Contains(captured[2], []byte("historical work")) || !bytes.Contains(captured[2], []byte("Continue the existing task directly"))) {
					t.Fatal("active window was not replaced")
				}
				if failure && !bytes.Contains(captured[2], []byte("historical work")) {
					t.Fatal("failure replaced old window")
				}
				resumed, err := openChatResources(context.Background(), cfg, root, string(thread), workspace)
				if err != nil {
					t.Fatal(err)
				}
				submitAppTurn(t, resumed, "after-resume")
				if err := resumed.close(context.Background()); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestAutomaticCompactionHeadlessModes(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, mode := range []headless.Mode{headless.ModeText, headless.ModeJSON, headless.ModeStreamJSON} {
			t.Run(string(family)+"/"+string(mode), func(t *testing.T) {
				var count atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					n := int(count.Add(1))
					w.Header().Set("Content-Type", "text/event-stream")
					if family == domain.ProviderOpenAI {
						writeOpenAIAppTurn(w, n)
					} else {
						var data bytes.Buffer
						writeAnthropicAppTurn(&data, n)
						_, _ = w.Write(bytes.Replace(data.Bytes(), []byte("data: {\"type\":\"message_stop\"}"), []byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}"), 1))
					}
				}))
				defer server.Close()
				budget, _ := contextplan.NewBudget(20000, 0, 0)
				cfg := config.Config{Provider: config.Provider{Family: family, Model: "claude-test", BaseURL: server.URL, APIKey: secret.New("private-test-key")}, ContextBudget: budget}
				root := filepath.Join(t.TempDir(), "sessions")
				resource, err := openChatResources(context.Background(), cfg, root, "", t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				submitAppTurn(t, resource, strings.Repeat("historical work ", 3500))
				thread := resource.identity.ThreadID
				if err := resource.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(t.TempDir(), "config.json")
				content := fmt.Sprintf(`{"provider":%q,"base_url":%q,"api_key":"private-test-key","model":"claude-test","context_window_tokens":20000}`, family, server.URL)
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
				output := &appWriteCloser{}
				options := Options{Mode: mode, Prompt: "next", ResumeThreadID: string(thread), SessionDataRoot: root, SessionCatalogPath: filepath.Join(t.TempDir(), "catalog.sqlite"), ConfigPath: path, Output: output}
				if mode == headless.ModeStreamJSON {
					options.Prompt = ""
					options.Input = io.NopCloser(strings.NewReader("{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"next\",\"text\":\"next\"}\n"))
				}
				outcome := Run(context.Background(), options)
				if outcome.ExitCode() != 0 || count.Load() != 3 {
					t.Fatalf("outcome=%v requests=%d output=%s", outcome, count.Load(), output.String())
				}
				text := output.String()
				if mode == headless.ModeText && text != "answer-3\n" {
					t.Fatal("text output changed", text)
				}
				if mode != headless.ModeStreamJSON && strings.Contains(text, "compaction") {
					t.Fatal("one-shot control output leaked")
				}
				if mode == headless.ModeStreamJSON && (strings.Count(text, `"type":"compaction.started"`) != 1 || strings.Count(text, `"type":"compaction.completed"`) != 1 || !strings.Contains(text, `"trigger":"auto"`)) {
					t.Fatal("automatic stream boundary absent", text)
				}
				if strings.Contains(text, "private-test-key") || strings.Contains(text, "historical work") {
					t.Fatal("body or secret leaked")
				}
			})
		}
	}
}
