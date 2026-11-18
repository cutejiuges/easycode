package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"easycode/internal/config"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/secret"
)

func TestAutomaticCompactionRealProvidersAndResume(t *testing.T) {
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
					if isSummary {
						wire := reasoningFixtureStream(family, n, false)
						if family == domain.ProviderAnthropic {
							wire = bytes.Replace(wire, []byte(`data: {"type":"message_stop"}`), []byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}"), 1)
						}
						_, _ = w.Write(wire)
						return
					}

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
				// 十工具目录增加了摘要请求开销；窗口须覆盖输入和 4096 输出预留，旧历史仍超过软阈值。
				budget, _ := contextplan.NewBudget(22000, 0, 0)
				cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, Model: "claude-test", BaseURL: server.URL, APIKey: secret.New("private-test-key")}, ContextBudget: budget}, false)
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
