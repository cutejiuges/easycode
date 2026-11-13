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
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/config"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/secret"
	"easycode/internal/tool/builtin"
)

type appStopRunner struct{ runs, preflights int }

func (r *appStopRunner) Preflight(context.Context) domain.HookCode {
	r.preflights++
	return domain.HookOK
}
func (r *appStopRunner) Run(_ context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, input domain.HookInput, _ time.Duration) domain.HookExecution {
	r.runs++
	s, _ := input.StopInput()
	result := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
	if s.Sample().SampleIndex == 0 {
		result.Status, result.Code = domain.HookBlock, domain.HookDenied
		feedback, _ := domain.NewHookFeedbackCandidate("exact-blocking-reason omitted by summary")
		return domain.HookExecution{Result: result, Feedback: feedback}
	}
	return domain.HookExecution{Result: result}
}
func serveStopAssistant(w io.Writer, family domain.ProviderFamily, text string) {
	encoded, _ := codec.MarshalStable(text)
	if family == domain.ProviderOpenAI {
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"stop-response\"}}\n\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":%s}]}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"stop-response\"}}\n\n", encoded)
	} else {
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"stop-message\",\"model\":\"fixture-model\"}}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n", encoded)
	}
}

func TestStopCompactionKeepsFrozenFeedbackAndGenuineSkillCorrection(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, mode := range []string{"summary", "failed-summary", "empty-summary", "cancel-summary"} {
			t.Run(string(family)+"/"+mode, func(t *testing.T) {
				workspace := t.TempDir()
				root := filepath.Join(t.TempDir(), "sessions")
				writeStopHookConfig(t, workspace, "true", domain.HookFeedbackModel)
				skillPath := filepath.Join(workspace, ".easycode", "skills", "review", "SKILL.md")
				if err := os.MkdirAll(filepath.Dir(skillPath), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(skillPath, []byte("---\ndescription: Review local changes\n---\nFrozen correction instructions $ARGUMENTS"), 0600); err != nil {
					t.Fatal(err)
				}
				var mu sync.Mutex
				var requests [][]byte
				var host *chatRuntime.ChatSession
				summaries := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					b, _ := io.ReadAll(r.Body)
					mu.Lock()
					requests = append(requests, b)
					n := len(requests)
					mu.Unlock()
					isSummary := bytes.Contains(b, []byte(`"tool_choice":"none"`)) || bytes.Contains(b, []byte(`"tool_choice":{"type":"none"}`))
					w.Header().Set("Content-Type", "text/event-stream")
					if isSummary {
						mu.Lock()
						summaries++
						mu.Unlock()
						if bytes.Contains(b, []byte("exact-blocking-reason")) || bytes.Contains(b, []byte("exact-user-correction")) || bytes.Contains(b, []byte("Frozen correction instructions")) {
							t.Error("pending candidate entered old-window summary")
						}
						if !bytes.Contains(b, []byte("historical work")) {
							t.Error("old committed sample absent from summary")
						}
						if mode == "failed-summary" {
							http.Error(w, "private-summary-error", http.StatusBadRequest)
							return
						}
						if mode == "cancel-summary" {
							host.Interrupt()
							<-r.Context().Done()
							return
						}
						text := "goal; completed work; next step"
						if mode == "empty-summary" {
							text = ""
						}
						serveStopAssistant(w, family, text)
						return
					}
					text := "final answer"
					if n == 1 {
						text = strings.Repeat("historical work ", 3000)
					}
					serveStopAssistant(w, family, text)
				}))
				defer server.Close()
				budget, _ := contextplan.NewBudget(20000, 0, 0)
				cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("fixture-key")}, ContextBudget: budget}, true)
				runner := &appStopRunner{}
				hooks := chatResourceHooks{interactiveApprovals: true, newHookRunner: func(*builtin.Workspace, builtin.CommandEnvironment, builtin.HookOutputParser) (chatRuntime.HookRunner, error) {
					return runner, nil
				}}
				resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, "", false, workspace, hooks)
				if err != nil {
					t.Fatal(err)
				}
				defer resources.close(context.Background())
				host, err = startTUIControlSession(context.Background(), resources.runtime)
				if err != nil {
					t.Fatal(err)
				}
				defer host.Shutdown(context.Background())
				events, err := submitInteractiveFixture(t, host, context.Background(), "complete the task")
				if err != nil {
					t.Fatal(err)
				}
				var correction domain.InputID
				committed := 0
				for event := range events {
					switch event.Kind {
					case protocol.EventHookTrustRequested:
						input, err := host.Append(context.Background(), "/review exact-user-correction\n  preserve this indentation")
						if err != nil {
							t.Fatal(err)
						}
						correction = input.EntryID()
						p, _ := protocol.DecodeHookTrustRequested(event)
						if err := host.ResolveHookTrust(context.Background(), event.TurnID, p, protocol.HookTrustSession); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(skillPath, []byte("---\ndescription: Changed\n---\nCHANGED-AFTER-CLAIM"), 0600); err != nil {
							t.Fatal(err)
						}
					case protocol.EventUserInputCommitted:
						p, _ := protocol.DecodeUserInputCommitted(event)
						if p.InputID == correction {
							committed++
						}
					}
				}
				if err := host.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
				thread := resources.identity.ThreadID
				path, _ := resources.service.repository.JournalPath(thread)
				if err := resources.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				mu.Lock()
				captured := append([][]byte(nil), requests...)
				summaryCount := summaries
				mu.Unlock()
				if summaryCount != 1 {
					t.Fatalf("expected one summary, requests=%d summaries=%d", len(captured), summaryCount)
				}
				wantNetwork := 3
				if mode == "cancel-summary" {
					wantNetwork = 2
				}
				if len(captured) != wantNetwork {
					t.Fatal("unexpected retry/ordinary request", len(captured))
				}
				_, plan := loadToolLoopPlan(t, root, thread)
				if plan.StopRecovery != nil || len(plan.Stops) < 1 || plan.Stops[0].DeliverySequence == 0 {
					t.Fatal("accepted feedback was abandoned")
				}
				if mode == "cancel-summary" {
					if committed != 0 {
						t.Fatal("unadmitted correction committed on cancellation")
					}
				} else {
					if committed != 1 || !bytes.Contains(captured[2], []byte("exact-blocking-reason")) || !bytes.Contains(captured[2], []byte("exact-user-correction")) || !bytes.Contains(captured[2], []byte("Frozen correction instructions")) || bytes.Contains(captured[2], []byte("CHANGED-AFTER-CLAIM")) {
						t.Fatal("frozen candidate lost or resampled", committed)
					}
					if mode == "summary" && bytes.Contains(captured[2], []byte("historical work")) {
						t.Fatal("checkpoint did not replace old window")
					}
				}
				if len(plan.Compactions) != 1 {
					t.Fatal("summary terminal missing")
				}
				if mode == "summary" {
					raw, _ := os.ReadFile(path)
					testStopRecoveryWindows(t, cfg, root, workspace, path, thread, raw)
				}
			})
		}
	}
}
