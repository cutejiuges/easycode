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
	"reflect"
	"strings"
	"sync"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool"
	"easycode/internal/tool/builtin"
)

func TestFailureBothProvidersRealHookSandboxAndFrozenRestore(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, cap := range []domain.HookCapability{domain.HookPatch, domain.HookWrite, domain.HookExec} {
			for _, mode := range []string{"model", "none", "invalid", "exit-two", "unmatched", "deny", "ordinary-deny", "empty", "wrong-event", "oversized", "escaped-budget"} {
				t.Run(string(family)+"/"+string(cap)+"/"+mode, func(t *testing.T) {
					workspace := t.TempDir()
					if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("old\n"), 0600); err != nil {
						t.Fatal(err)
					}
					command := `cat >/dev/null; printf 'RAW-PRIVATE-HOOK-STDERR' >&2; printf '%s' '{"hookSpecificOutput":{"hookEventName":"PostToolUseFailure","additionalContext":"Authorized diagnosis: inspect the original failure."}}'`
					policy := domain.HookFeedbackModel
					if mode == "none" {
						policy = domain.HookFeedbackNone
					}
					if mode == "invalid" {
						command = "cat >/dev/null; printf RAW-PRIVATE-REJECTED-STDOUT"
					}
					if mode == "exit-two" {
						command = "cat >/dev/null; exit 2"
					}
					if mode == "empty" {
						command = "cat >/dev/null; printf '{}'"
					}
					if mode == "wrong-event" {
						command = `cat >/dev/null; printf '%s' '{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"REJECTED-WRONG-EVENT-BODY"}}'`
					}
					if mode == "oversized" || mode == "escaped-budget" {
						content := strings.Repeat("x", domain.MaxHookFeedbackBytes+1)
						if mode == "escaped-budget" {
							content = strings.Repeat("<", domain.MaxHookFeedbackBytes)
						}
						command = `cat >/dev/null; printf '%s' '{"hookSpecificOutput":{"hookEventName":"PostToolUseFailure","additionalContext":"` + content + `"}}'`
					}
					if mode == "unmatched" {
						writePostHookConfig(t, workspace, "exit 9", domain.HookFeedbackModel)
					} else {
						writeFailureHookConfig(t, workspace, command, policy)
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
						if n == 1 {
							var wire bytes.Buffer
							serveHookToolSample(&wire, family, cap)
							_, _ = w.Write(withReasoningFixture(family, wire.Bytes()))
						} else {
							serveWriteScenarioSample(w, family, n, nil)
						}
					}))
					defer server.Close()
					cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("private-fixture-key")}}, true)
					root := filepath.Join(t.TempDir(), "sessions")
					failedExecutor := &spawnFailureExec{}
					hooks := chatResourceHooks{interactiveApprovals: true}
					if cap == domain.HookExec {
						hooks.newExecExecutor = func(*builtin.Workspace, builtin.CommandEnvironment, tool.ExecOutputRenderer) (tool.ExecExecutor, error) {
							return failedExecutor, nil
						}
					}
					resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, "", false, workspace, hooks)
					if err != nil {
						t.Fatal(err)
					}
					defer resources.close(context.Background())
					control, err := startTUIControlSession(context.Background(), resources.runtime)
					if err != nil {
						t.Fatal(err)
					}
					defer control.Shutdown(context.Background())
					events, err := submitInteractiveFixture(t, control, context.Background(), "diagnose failure")
					if err != nil {
						t.Fatal(err)
					}
					for e := range events {
						switch e.Kind {
						case protocol.EventHookTrustRequested:
							p, _ := protocol.DecodeHookTrustRequested(e)
							decision := protocol.HookTrustSession
							if mode == "deny" {
								decision = protocol.HookTrustDeny
							}
							if err := control.ResolveHookTrust(context.Background(), e.TurnID, p, decision); err != nil {
								t.Fatal(err)
							}
						case protocol.EventToolApprovalRequested:
							if cap != domain.HookExec && mode != "ordinary-deny" {
								path := "main.go"
								if cap == domain.HookWrite {
									path = "written"
								}
								if err := os.WriteFile(filepath.Join(workspace, path), []byte("external change\n"), 0600); err != nil {
									t.Fatal(err)
								}
							}
							p, _ := protocol.DecodeToolApprovalRequested(e)
							decision := protocol.ApprovalAllowOnce
							if mode == "ordinary-deny" {
								decision = protocol.ApprovalDeny
							}
							if err := control.ResolveApproval(context.Background(), e.TurnID, p, decision, ""); err != nil {
								t.Fatal(err)
							}
						case protocol.EventTurnFailed:
							t.Fatalf("failure check broke turn: %s", e.Payload)
						}
					}
					if err := control.Shutdown(context.Background()); err != nil {
						t.Fatal(err)
					}
					thread := resources.identity.ThreadID
					path, _ := resources.service.repository.JournalPath(thread)
					if err := resources.close(context.Background()); err != nil {
						t.Fatal(err)
					}
					raw, _ := os.ReadFile(path)
					_, plan := loadToolLoopPlan(t, root, thread)
					if len(plan.Tools) != 1 || plan.Tools[0].PostHooks == nil || !plan.Tools[0].HasResult {
						t.Fatal("lost tool/post facts")
					}
					entry := plan.Tools[0]
					g := entry.PostHooks
					wantStatus := tool.ResultConflict
					if cap == domain.HookExec {
						wantStatus = tool.ResultError
					}
					if mode == "deny" || mode == "ordinary-deny" {
						wantStatus = tool.ResultDenied
					}
					if entry.Result.Status() != wantStatus || g.Terminal == nil {
						t.Fatal("post changed original failure", entry.Result.Status())
					}
					wantCode := domain.HookOK
					switch mode {
					case "none":
						wantCode = domain.HookFeedbackNotAllowed
					case "invalid", "wrong-event", "oversized":
						wantCode = domain.HookOutputInvalid
					case "exit-two":
						wantCode = domain.HookExitFailed
					case "unmatched":
						wantCode = domain.HookEventUnmatched
					case "deny":
						wantCode = domain.HookTrustDenied
					case "ordinary-deny":
						wantCode = domain.HookToolNotSuccessful
					case "escaped-budget":
						wantCode = domain.HookFeedbackBudgetExceeded
					}
					if g.Terminal.Code != wantCode {
						t.Fatal("wrong post outcome", g.Terminal.Code, wantCode)
					}
					wantRoute := domain.PostHookRouteFailure
					if mode == "deny" || mode == "ordinary-deny" {
						wantRoute = domain.PostHookRouteNone
					}
					if g.Terminal.Route != wantRoute {
						t.Fatal("wrong failure route")
					}
					runs := 1
					if mode == "unmatched" || mode == "deny" || mode == "ordinary-deny" {
						runs = 0
					}
					if entry.Result.Budget().Allowance() != g.Plan.Allowance-g.Plan.Reservation || g.Terminal.Budget.FeedbackActual > g.Plan.Reservation {
						t.Fatal("post budget differs from frozen plan")
					}
					if mode == "empty" && (g.Terminal.Budget.FeedbackActual != 0 || g.Finished[0].Feedback.Kind != domain.HookFeedbackEmpty) {
						t.Fatal("empty output disclosed feedback")
					}
					if mode == "escaped-budget" && (g.Finished[0].Feedback.Kind != domain.HookFeedbackCode || g.Finished[0].Feedback.Content != "") {
						t.Fatal("escaped over-budget body was not rejected whole")
					}
					if len(g.Finished) != runs || cap == domain.HookExec && failedExecutor.calls != map[bool]int{true: 0, false: 1}[mode == "deny" || mode == "ordinary-deny"] {
						t.Fatal("duplicate or unselected execution")
					}
					mu.Lock()
					captured := append([][]byte(nil), requests...)
					mu.Unlock()
					if len(captured) != 2 || !bytes.Contains(captured[1], []byte(entry.Result.Code())) {
						t.Fatal("original error not sent to provider")
					}
					disclosed := bytes.Contains(captured[1], []byte("Authorized diagnosis:"))
					if disclosed != (mode == "model") || bytes.Contains(raw, []byte("Authorized diagnosis:")) != disclosed {
						t.Fatal("unauthorized/lost feedback")
					}
					for _, private := range []string{"RAW-PRIVATE-HOOK-STDERR", "RAW-PRIVATE-REJECTED-STDOUT", "private-fixture-key", "REJECTED-WRONG-EVENT-BODY", strings.Repeat("x", domain.MaxHookFeedbackBytes+1), strings.Repeat("<", domain.MaxHookFeedbackBytes)} {
						if bytes.Contains(raw, []byte(private)) || bytes.Contains(bytes.Join(captured, nil), []byte(private)) {
							t.Fatal("secret/raw stream leaked", private)
						}
					}
					if mode == "model" {
						if err := os.Remove(filepath.Join(workspace, ".easycode", "hooks.json")); err != nil {
							t.Fatal(err)
						}
						copyRoot := filepath.Join(t.TempDir(), "sessions")
						repo, err := session.OpenOrCreateRepository(copyRoot)
						if err != nil {
							t.Fatal(err)
						}
						lease, err := repo.Create(context.Background(), thread)
						if err != nil {
							t.Fatal(err)
						}
						lease.Close()
						copyPath, _ := repo.JournalPath(thread)
						if err := os.WriteFile(copyPath, raw, 0600); err != nil {
							t.Fatal(err)
						}
						repo.Close()
						live, err := openChatResources(context.Background(), cfg, root, string(thread), workspace)
						if err != nil {
							t.Fatal(err)
						}
						defer live.close(context.Background())
						restored, err := openChatResources(context.Background(), cfg, copyRoot, string(thread), workspace)
						if err != nil {
							t.Fatal(err)
						}
						defer restored.close(context.Background())
						lf, _ := live.transcriptQueries.projector.(provider.Conversation).HistoryFootprint()
						rf, _ := restored.transcriptQueries.projector.(provider.Conversation).HistoryFootprint()
						if !reflect.DeepEqual(lf, rf) {
							t.Fatal("failure restore changed footprint")
						}
						submitAppTurn(t, live, "continue diagnostics")
						submitAppTurn(t, restored, "continue diagnostics")
						mu.Lock()
						a, b := bytes.Clone(requests[len(requests)-2]), bytes.Clone(requests[len(requests)-1])
						mu.Unlock()
						if !bytes.Equal(a, b) || !strings.Contains(string(a), "opaque-reasoning-sentinel") {
							t.Fatal("failure restore changed native bytes/order")
						}
						assertRequestFingerprintEqual(t, a, b)
					}
				})
			}
		}
	}
}
