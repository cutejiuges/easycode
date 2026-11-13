//go:build darwin

package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
)

func TestStopBothProvidersRealRunnerSameTurnAndRestore(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, mode := range []string{"twice", "third-block", "none", "exit-two", "deny", "pass", "invalid"} {
			t.Run(string(family)+"/"+mode, func(t *testing.T) {
				workspace := t.TempDir()
				command := `data=$(cat); printf 'RAW-PRIVATE-HOOK-STDERR' >&2; case "$data" in *'"sample_index":2'*) printf '{}' ;; *) printf '%s' '{"decision":"block","reason":"  Verify the missing behavior.\n"}' ;; esac`
				policy := domain.HookFeedbackModel
				if mode == "third-block" {
					command = `cat >/dev/null; printf '%s' '{"decision":"block","reason":"Verify the missing behavior."}'`
				}
				if mode == "none" {
					policy = domain.HookFeedbackNone
				}
				if mode == "exit-two" {
					command = "cat >/dev/null; printf RAW-PRIVATE-HOOK-STDERR >&2; exit 2"
				}
				if mode == "pass" {
					command = "cat >/dev/null; printf '{}'"
				}
				if mode == "invalid" {
					command = "cat >/dev/null; printf RAW-PRIVATE-HOOK-STDOUT"
				}
				writeStopHookConfig(t, workspace, command, policy)
				var mu sync.Mutex
				var requests [][]byte
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					b, _ := io.ReadAll(r.Body)
					mu.Lock()
					requests = append(requests, b)
					mu.Unlock()
					w.Header().Set("Content-Type", "text/event-stream")
					serveWriteScenarioSample(w, family, 2, nil)
				}))
				defer server.Close()
				cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("private-fixture-key")}}, true)
				root := filepath.Join(t.TempDir(), "sessions")
				resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{interactiveApprovals: true})
				if err != nil {
					t.Fatal(err)
				}
				defer resources.close(context.Background())
				control, err := startTUIControlSession(context.Background(), resources.runtime)
				if err != nil {
					t.Fatal(err)
				}
				defer control.Shutdown(context.Background())
				events, err := submitInteractiveFixture(t, control, context.Background(), "complete the task")
				if err != nil {
					t.Fatal(err)
				}
				trusts, completed, failed := 0, 0, 0
				var turn domain.TurnID
				for event := range events {
					if event.TurnID.Valid() {
						if turn != "" && turn != event.TurnID {
							t.Fatal("continuation created a new turn")
						}
						turn = event.TurnID
					}
					switch event.Kind {
					case protocol.EventHookTrustRequested:
						trusts++
						p, err := protocol.DecodeHookTrustRequested(event)
						if err != nil {
							t.Fatal(err)
						}
						decision := protocol.HookTrustSession
						if mode == "deny" {
							decision = protocol.HookTrustDeny
						}
						if err := control.ResolveHookTrust(context.Background(), event.TurnID, p, decision); err != nil {
							t.Fatal(err)
						}
					case protocol.EventTurnCompleted:
						completed++
					case protocol.EventTurnFailed:
						failed++
						t.Log(string(event.Payload))
					}
				}
				if err := control.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
				thread := resources.identity.ThreadID
				journalPath, _ := resources.service.repository.JournalPath(thread)
				if err := resources.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				raw, err := os.ReadFile(journalPath)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(raw, []byte("RAW-PRIVATE")) || bytes.Contains(raw, []byte("private-fixture-key")) {
					t.Fatal("private output leaked")
				}
				_, plan := loadToolLoopPlan(t, root, thread)
				wantSamples := 1
				if mode == "twice" || mode == "third-block" {
					wantSamples = 3
				}
				mu.Lock()
				savedRequests := append([][]byte(nil), requests...)
				mu.Unlock()
				if len(savedRequests) != wantSamples || len(plan.Stops) != wantSamples || len(plan.Tools) != 0 || trusts != 1 || completed+failed != 1 {
					t.Fatalf("invalid counts requests=%d stops=%d trust=%d terminal=%d/%d", len(savedRequests), len(plan.Stops), trusts, completed, failed)
				}
				if mode == "twice" || mode == "pass" {
					if completed != 1 {
						t.Fatal("expected successful completion")
					}
				} else if failed != 1 {
					t.Fatal("expected terminal rejection")
				}
				for i, state := range plan.Stops {
					if state.Plan.Sample().TurnID != turn || state.Plan.Sample().SampleIndex != uint32(i) || state.Decision == nil {
						t.Fatal("lost exact sample")
					}
					if i < 2 && wantSamples == 3 {
						feedback, ok := state.Decision.Feedback()
						if !ok || state.DeliverySequence == 0 || !bytes.Contains(savedRequests[i+1], []byte("Verify the missing behavior.")) || !strings.Contains(feedback.Text, "completion_check_feedback") {
							t.Fatal("feedback not consumed")
						}
					}
				}
				if mode == "none" && bytes.Contains(raw, []byte("Verify the missing behavior")) {
					t.Fatal("unapproved reason leaked into journal")
				}
				before := append([]byte(nil), raw...)
				if mode == "twice" {
					testStopRecoveryWindows(t, cfg, root, workspace, journalPath, thread, before)
				}
				// 配置已删除仍能先恢复原事实，恢复不会重跑旧脚本或发送请求。
				if err := os.Remove(filepath.Join(workspace, ".easycode", "hooks.json")); err != nil {
					t.Fatal(err)
				}
				resumed, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, string(thread), false, workspace, chatResourceHooks{interactiveApprovals: true})
				if err != nil {
					t.Fatal(err)
				}
				if err := resumed.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				after, _ := os.ReadFile(journalPath)
				if !bytes.Equal(before, after) {
					t.Fatal("completed restore changed journal")
				}
				mu.Lock()
				count := len(requests)
				mu.Unlock()
				if count != wantSamples {
					t.Fatal("restore made network request")
				}
			})
		}
	}
}
