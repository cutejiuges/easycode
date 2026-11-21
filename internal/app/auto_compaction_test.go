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
	"time"

	"easycode/internal/config"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
)

func TestAutomaticCompactionRealProvidersAndResume(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, scenario := range []string{"accepted", "provider_rejected", "headless_match", "headless_over_budget"} {
			name := string(family) + "/" + scenario
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				overBudget := scenario == "headless_over_budget"
				headless := scenario == "headless_match" || overBudget
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
						if n == 2 && !bytes.Contains(body, []byte(preCompactFixtureFocus)) {
							t.Error("automatic summary lost approved focus")
						}
						if scenario == "provider_rejected" {
							http.Error(w, "private-error-body", http.StatusRequestEntityTooLarge)
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
				runner := &compactionFixtureRunner{t: t, automatic: true, post: scenario == "accepted"}
				hooks := compactionFixtureHooks(t, runner, "auto")
				hooks.interactiveApprovals = !headless
				resources, err := openChatResourcesWithHooks(ctx, cfg, dataPaths{sessionRoot: root}, "", false, workspace, hooks)
				if err != nil {
					t.Fatal(err)
				}
				first := "latest-prior-correction " + strings.Repeat("historical work ", 3500)
				inputs := []string{first, "new-exact-correction\n  keep this indentation  "}
				if overBudget {
					submitAppTurn(t, resources, first)
					events, err := resources.session.Submit(ctx, inputs[1]+strings.Repeat("oversized correction ", 8000))
					if err != nil {
						t.Fatal(err)
					}
					var rejected bool
					for event := range events {
						if event.Kind == protocol.EventTurnFailed {
							rejected = bytes.Contains(event.Payload, []byte(`"context_limit_exceeded"`))
						}
					}
					if err := resources.close(ctx); err != nil {
						t.Fatal(err)
					}
					mu.Lock()
					count := len(requests)
					mu.Unlock()
					if !rejected || count != 1 || runner.runs != 0 {
						t.Fatal("over-budget fallback reached provider or hook runner")
					}
					return
				}
				if headless {
					for _, input := range inputs {
						submitAppTurn(t, resources, input)
					}
				} else {
					if err := resources.session.StartInteractive(ctx); err != nil {
						t.Fatal(err)
					}
					for _, input := range inputs {
						submitCheckedAutoTurn(t, ctx, resources, input)
					}
				}
				thread := resources.identity.ThreadID
				if err := resources.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				mu.Lock()
				count, summaryCount := len(requests), summaries
				captured := append([][]byte(nil), requests...)
				mu.Unlock()
				wantSummary := 1
				if headless {
					wantSummary = 0
				}
				wantPost := 0
				if runner.post {
					wantPost = 1
				}
				if count != 2+wantSummary || summaryCount != wantSummary || runner.runs != wantSummary+wantPost || runner.postRuns != wantPost {
					t.Fatalf("requests=%d summaries=%d checks=%d", count, summaryCount, runner.runs)
				}
				next := captured[count-1]
				if !bytes.Contains(next, []byte("new-exact-correction\\n  keep this indentation  ")) || bytes.Contains(next, []byte(preCompactFixtureFocus)) {
					t.Fatal("input text lost")
				}
				if scenario == "accepted" && (bytes.Contains(next, []byte("historical work")) || !bytes.Contains(next, []byte("Continue the existing task directly"))) {
					t.Fatal("active window was not replaced")
				}
				if scenario != "accepted" && !bytes.Contains(next, []byte("historical work")) {
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

func submitCheckedAutoTurn(t *testing.T, ctx context.Context, resources *chatResources, input string) {
	t.Helper()
	if result, err := appendInteractiveText(ctx, resources.session, input); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatalf("turn admission: %v", err)
	}
	for {
		select {
		case item, open := <-resources.session.Outputs():
			if !open {
				t.Fatal("closed before turn terminal")
			}
			if event, ok := item.ContextEvent(); ok && event.Kind == protocol.EventHookTrustRequested {
				request, err := protocol.DecodeHookTrustRequested(event)
				if err != nil || !event.TurnID.Valid() || !event.CompactionID.Valid() {
					t.Fatalf("automatic review lost scope: %v", err)
				}
				if err := resources.session.ResolveHookTrust(ctx, event.TurnID, request, protocol.HookTrustSession); err != nil {
					t.Fatal(err)
				}
			}
			if event, ok := item.TurnEvent(); ok {
				if event.Kind == protocol.EventTurnFailed {
					t.Fatalf("turn failed: %s", event.Payload)
				}
				if event.Kind == protocol.EventTurnCompleted {
					return
				}
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
