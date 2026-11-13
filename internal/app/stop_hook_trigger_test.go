package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool/builtin"
)

// TestStopNeverChecksFailedOrCancelledProviderSamples 验证已有 Stop 配置不会把失败样本当作完成答复。
func TestStopNeverChecksFailedOrCancelledProviderSamples(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, scenario := range []string{"http-error", "provider-error", "missing-terminal", "cancel-partial"} {
			t.Run(string(family)+"/"+scenario, func(t *testing.T) {
				workspace := t.TempDir()
				writeStopHookConfig(t, workspace, "printf forbidden > stop-marker", domain.HookFeedbackModel)
				var requests atomic.Int32
				partialSent := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					_, _ = io.Copy(io.Discard, r.Body)
					if scenario == "http-error" {
						http.Error(w, "PRIVATE-STOP-API-ERROR", http.StatusBadRequest)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					if scenario == "provider-error" {
						if family == domain.ProviderOpenAI {
							_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"failed-response\"}}\n\ndata: {\"type\":\"response.failed\",\"response\":{\"id\":\"failed-response\",\"error\":{\"message\":\"PRIVATE-STOP-API-ERROR\"}}}\n\n")
						} else {
							_, _ = io.WriteString(w, "data: {\"type\":\"error\",\"error\":{\"type\":\"api_error\",\"message\":\"PRIVATE-STOP-API-ERROR\"}}\n\n")
						}
						return
					}
					var response bytes.Buffer
					serveStopAssistant(&response, family, "INCOMPLETE-STOP-ANSWER")
					terminal := []byte("data: {\"type\":\"response.completed\"")
					if family == domain.ProviderAnthropic {
						terminal = []byte("data: {\"type\":\"message_stop\"")
					}
					body := response.Bytes()
					_, _ = w.Write(body[:bytes.LastIndex(body, terminal)])
					if scenario == "cancel-partial" {
						w.(http.Flusher).Flush()
						close(partialSent)
						<-r.Context().Done()
					}
				}))
				defer server.Close()
				cfg := config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("private-stop-fixture-key")}}
				root := filepath.Join(t.TempDir(), "sessions")
				runner := &appHookRunner{}
				resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{
					interactiveApprovals: true,
					newHookRunner: func(*builtin.Workspace, builtin.CommandEnvironment, builtin.HookOutputParser) (chatRuntime.HookRunner, error) {
						return runner, nil
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				defer resources.close(context.Background())
				host, err := startTUIControlSession(context.Background(), resources.runtime)
				if err != nil {
					t.Fatal(err)
				}
				defer host.Shutdown(context.Background())
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				events, err := submitInteractiveFixture(t, host, ctx, "finish the task")
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "cancel-partial" {
					select {
					case <-partialSent:
						host.Interrupt()
					case <-ctx.Done():
						t.Fatal("provider did not publish the incomplete sample")
					}
				}
				trusts, completed, failed, stopEvents := 0, 0, 0, 0
				wantCode := fault.CodeProviderRequest
				if scenario == "missing-terminal" {
					wantCode = fault.CodeStreamProtocol
				} else if scenario == "cancel-partial" {
					wantCode = fault.CodeUserCancelled
				}
				for event := range events {
					switch event.Kind {
					case protocol.EventHookTrustRequested:
						trusts++
						p, err := protocol.DecodeHookTrustRequested(event)
						if err != nil {
							t.Fatal(err)
						}
						if err := host.ResolveHookTrust(context.Background(), event.TurnID, p, protocol.HookTrustDeny); err != nil {
							t.Fatal(err)
						}
					case protocol.EventStopHookCommitted:
						stopEvents++
					case protocol.EventTurnCompleted:
						completed++
					case protocol.EventTurnFailed:
						failed++
						p, err := protocol.DecodeTurnFailed(event)
						if err != nil || p.Code != string(wantCode) || p.Cancelled != (scenario == "cancel-partial") {
							t.Fatalf("unexpected sample failure: %+v %v", p, err)
						}
					}
				}
				if err := host.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
				thread := resources.identity.ThreadID
				journalPath, err := resources.service.repository.JournalPath(thread)
				if err != nil {
					t.Fatal(err)
				}
				if err := resources.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				if requests.Load() != 1 || runner.runs.Load() != 0 || trusts != 0 || stopEvents != 0 || completed != 0 || failed != 1 {
					t.Fatalf("failed sample reached completion checks: requests=%d runs=%d trust=%d stop=%d completed=%d failed=%d", requests.Load(), runner.runs.Load(), trusts, stopEvents, completed, failed)
				}
				loaded, plan := loadToolLoopPlan(t, root, thread)
				if len(plan.Stops) != 0 || plan.StopRecovery != nil || len(plan.NativeCommits) != 1 || len(plan.SampleUsages) != 0 || len(plan.Tools) != 0 || plan.InterruptedTail != nil {
					t.Fatal("incomplete sample created durable completion facts")
				}
				for _, record := range loaded.Records {
					if record.EventKind == session.EventTurnStopHookPlanned || record.EventKind == session.EventTurnStopHookDecided || record.EventKind == session.EventHookExecutionStarted || record.EventKind == session.EventHookExecutionFinished {
						t.Fatal("failed sample persisted a completion check", record.EventKind)
					}
				}
				raw, err := os.ReadFile(journalPath)
				if err != nil || bytes.Contains(raw, []byte("PRIVATE-STOP-API-ERROR")) || bytes.Contains(raw, []byte("private-stop-fixture-key")) || bytes.Contains(raw, []byte("INCOMPLETE-STOP-ANSWER")) {
					t.Fatal("incomplete sample or private error entered journal", err)
				}
				if _, err := os.Stat(filepath.Join(workspace, "stop-marker")); !os.IsNotExist(err) {
					t.Fatal("failed sample executed a Stop script")
				}
			})
		}
	}
}
