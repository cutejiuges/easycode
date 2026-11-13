package app

import (
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
	"easycode/internal/protocol"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/secret"
	"easycode/internal/tool/builtin"
)

type limitStopRunner struct {
	runs       int
	blockIndex uint32
	pass       bool
}

func (*limitStopRunner) Preflight(context.Context) domain.HookCode { return domain.HookOK }
func (f *limitStopRunner) Run(_ context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, input domain.HookInput, _ time.Duration) domain.HookExecution {
	f.runs++
	sample, _ := input.StopInput()
	result := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
	if !f.pass && sample.Sample().SampleIndex == f.blockIndex {
		result.Status, result.Code = domain.HookBlock, domain.HookDenied
		feedback, _ := domain.NewHookFeedbackCandidate("Verify completed reads")
		return domain.HookExecution{Result: result, Feedback: feedback}
	}
	return domain.HookExecution{Result: result}
}
func TestStopSampleAndCallLimitsKeepTextContinuationAvailable(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, mode := range []string{"last-pass", "last-block", "64-calls"} {
			t.Run(string(family)+"/"+mode, func(t *testing.T) {
				workspace := t.TempDir()
				root := filepath.Join(t.TempDir(), "sessions")
				if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("source\n"), 0600); err != nil {
					t.Fatal(err)
				}
				writeStopHookConfig(t, workspace, "true", domain.HookFeedbackModel)
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					n := int(requests.Add(1))
					w.Header().Set("Content-Type", "text/event-stream")
					count := 0
					if mode == "64-calls" && n == 2 {
						count = 64
					}
					if mode != "64-calls" && n < 16 {
						count = 1
					}
					var calls []writeScenarioCall
					for range count {
						calls = append(calls, writeScenarioCall{"Read", `{"file_path":"README.md"}`})
					}
					if mode == "64-calls" && len(calls) > 0 {
						calls[len(calls)-1] = writeScenarioCall{"Write", `{"file_path":"repaired.txt","content":"repair completed\n"}`}
					}
					serveWriteScenarioSample(w, family, n, calls)
				}))
				defer server.Close()
				runner := &limitStopRunner{blockIndex: 15, pass: mode == "last-pass"}
				if mode == "64-calls" {
					runner.blockIndex = 0
				}
				cfg := config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("fixture-key")}}
				resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{interactiveApprovals: true, newHookRunner: func(*builtin.Workspace, builtin.CommandEnvironment, builtin.HookOutputParser) (chatRuntime.HookRunner, error) {
					return runner, nil
				}})
				if err != nil {
					t.Fatal(err)
				}
				defer resources.close(context.Background())
				control, err := startTUIControlSession(context.Background(), resources.runtime)
				if err != nil {
					t.Fatal(err)
				}
				defer control.Shutdown(context.Background())
				events, err := submitInteractiveFixture(t, control, context.Background(), "read and verify")
				if err != nil {
					t.Fatal(err)
				}
				completed, failed := 0, 0
				for event := range events {
					switch event.Kind {
					case protocol.EventHookTrustRequested:
						p, _ := protocol.DecodeHookTrustRequested(event)
						if err := control.ResolveHookTrust(context.Background(), event.TurnID, p, protocol.HookTrustSession); err != nil {
							t.Fatal(err)
						}
					case protocol.EventToolApprovalRequested:
						p, _ := protocol.DecodeToolApprovalRequested(event)
						if err := control.ResolveApproval(context.Background(), event.TurnID, p, protocol.ApprovalAllowOnce, ""); err != nil {
							t.Fatal(err)
						}
					case protocol.EventTurnCompleted:
						completed++
					case protocol.EventTurnFailed:
						failed++
						p, _ := protocol.DecodeTurnFailed(event)
						if mode != "last-block" || p.Code != string(domain.HookSampleLimitExceeded) {
							t.Fatal("unexpected limit", p.Code)
						}
					}
				}
				if err := control.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
				thread := resources.identity.ThreadID
				if err := resources.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				_, plan := loadToolLoopPlan(t, root, thread)
				wantRequests, wantTools, wantRuns := 16, 15, 1
				if mode == "64-calls" {
					wantRequests, wantTools, wantRuns = 3, 64, 2
				}
				if requests.Load() != int32(wantRequests) || len(plan.Tools) != wantTools || runner.runs != wantRuns || completed+failed != 1 {
					t.Fatalf("counts %d/%d/%d", requests.Load(), len(plan.Tools), runner.runs)
				}
				if mode == "64-calls" {
					content, err := os.ReadFile(filepath.Join(workspace, "repaired.txt"))
					if err != nil || string(content) != "repair completed\n" || len(plan.Stops) != 2 || plan.Stops[0].Decision.Disposition != domain.StopContinued || plan.Stops[1].Plan.Sample().SampleIndex != 2 || plan.Stops[1].Plan.Continuations() != 1 {
						t.Fatal("Stop repair chain lost real tool effect, identity or continuation budget", err)
					}
				}
				if mode == "last-block" {
					state := plan.Stops[0]
					if state.Decision.Disposition != domain.StopBlocked || state.DeliverySequence != 0 || state.Decision.Continuations != 0 {
						t.Fatal("last sample created an impossible intent")
					}
				} else if completed != 1 {
					t.Fatal("text budget ended early")
				}
			})
		}
	}
}
