package app

import (
	"bytes"
	"context"
	"easycode/internal/provider"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/extension/hook"
	"easycode/internal/protocol"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool/builtin"
)

type appHookRunner struct {
	runs     atomic.Int32
	feedback bool
}

func (*appHookRunner) Preflight(context.Context) domain.HookCode { return domain.HookOK }
func (r *appHookRunner) Run(_ context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, _ domain.HookInput, _ time.Duration) domain.HookExecution {
	r.runs.Add(1)
	candidate := domain.HookFeedbackCandidate{}
	if r.feedback && id.Event.Post() {
		candidate, _ = domain.NewHookFeedbackCandidate("explicit check feedback")
	}
	return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}, Feedback: candidate}
}
func TestHookResumeReconcilesEveryBatchBeforeLoadingNewSnapshot(t *testing.T) {
	testHookResumeReconcilesEveryBatch(t, domain.HookPreToolUse)
}
func TestPostHookResumeReconcilesEveryBatchBeforeLoadingNewSnapshot(t *testing.T) {
	testHookResumeReconcilesEveryBatch(t, domain.HookPostToolUse)
}
func TestFailureHookResumeReconcilesEveryBatchBeforeLoadingNewSnapshot(t *testing.T) {
	testHookResumeReconcilesEveryBatch(t, domain.HookPostToolUseFailure)
}
func testHookResumeReconcilesEveryBatch(t *testing.T, event domain.HookEvent) {
	t.Helper()
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			workspace := t.TempDir()
			writeHookConfig(t, workspace, "cat >/dev/null; printf '{}'")
			file := filepath.Join(workspace, ".easycode", "hooks.json")
			b, _ := os.ReadFile(file)
			b = bytes.Replace(b, []byte(`"id":"check"`), []byte(`"id":"first"`), 1)
			var cfgObject struct {
				Hooks struct {
					PreToolUse []struct {
						Matcher string            `json:"matcher"`
						Hooks   []json.RawMessage `json:"hooks"`
					} `json:"PreToolUse"`
				} `json:"hooks"`
			}
			if err := json.Unmarshal(b, &cfgObject); err != nil {
				t.Fatal(err)
			}
			entry := cfgObject.Hooks.PreToolUse[0].Hooks[0]
			second := bytes.Replace(entry, []byte(`"id":"first"`), []byte(`"id":"second"`), 1)
			cfgObject.Hooks.PreToolUse[0].Hooks = append(cfgObject.Hooks.PreToolUse[0].Hooks, second)
			b, _ = json.Marshal(cfgObject)
			if event.Post() {
				b = bytes.ReplaceAll(b, []byte(`"PreToolUse"`), []byte(`"`+string(event)+`"`))
				b = bytes.ReplaceAll(b, []byte(`"type":"command"`), []byte(`"type":"command","feedback":"model"`))
			}
			if err := os.WriteFile(file, b, 0600); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			var requestMu sync.Mutex
			var captured [][]byte
			var compensatedRequest []byte
			var compensatedFootprint domain.NativeHistoryFootprint
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				requestMu.Lock()
				captured = append(captured, body)
				requestMu.Unlock()
				n := requests.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				var calls []writeScenarioCall
				if n == 1 {
					calls = []writeScenarioCall{{"Write", `{"file_path":"marker","content":"seed"}`}}
				}
				serveWriteScenarioSample(w, family, int(n), calls)
			}))
			defer server.Close()
			cfg := config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("fixture-key")}}
			root := filepath.Join(t.TempDir(), "sessions")
			runner := &appHookRunner{feedback: event.Post()}
			makeRunner := func(*builtin.Workspace, builtin.CommandEnvironment, builtin.HookOutputParser) (chatRuntime.HookRunner, error) {
				return runner, nil
			}
			resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{interactiveApprovals: true, newHookRunner: makeRunner})
			if err != nil {
				t.Fatal(err)
			}
			control, err := startTUIControlSession(context.Background(), resources.runtime)
			if err != nil {
				t.Fatal(err)
			}
			events, err := submitInteractiveFixture(t, control, context.Background(), "seed")
			if err != nil {
				t.Fatal(err)
			}
			for e := range events {
				switch e.Kind {
				case protocol.EventHookTrustRequested:
					p, _ := protocol.DecodeHookTrustRequested(e)
					if err := control.ResolveHookTrust(context.Background(), e.TurnID, p, protocol.HookTrustSession); err != nil {
						t.Fatal(err)
					}
				case protocol.EventToolApprovalRequested:
					if event == domain.HookPostToolUseFailure {
						if err := os.WriteFile(filepath.Join(workspace, "marker"), []byte("external change"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					p, _ := protocol.DecodeToolApprovalRequested(e)
					if err := control.ResolveApproval(context.Background(), e.TurnID, p, protocol.ApprovalAllowOnce, ""); err != nil {
						t.Fatal(err)
					}
				case protocol.EventTurnFailed:
					t.Fatal(string(e.Payload))
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
			baseline, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			loaded, _ := loadToolLoopPlan(t, root, thread)
			var checkpoints []session.Record
			admitted := false
			for _, r := range loaded.Records {
				if r.EventKind == session.EventToolCallReady {
					admitted = true
				}
				if admitted && r.BatchIndex+1 == r.BatchSize {
					checkpoints = append(checkpoints, r)
				}
			}
			for _, checkpoint := range checkpoints {
				t.Run(fmt.Sprintf("%s-%d", checkpoint.EventKind, checkpoint.Sequence), func(t *testing.T) {
					writeJournalPrefix(t, path, baseline, checkpoint.Sequence)
					prefix, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					// 新来源损坏和缺失都不能阻止旧计划先收口，也不能触发旧脚本重跑。
					if checkpoint.Sequence%2 == 0 {
						if err := os.WriteFile(file, []byte(`{"invalid":true}`), 0600); err != nil {
							t.Fatal(err)
						}
					} else {
						if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
							t.Fatal(err)
						}
					}
					loadCount := 0
					load := func(ctx context.Context, cwd, home string) (domain.HookSnapshot, error) {
						loadCount++
						raw, err := os.ReadFile(path)
						if err != nil {
							return domain.HookSnapshot{}, err
						}
						lines := bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n"))
						last, err := session.DecodeRecord(lines[len(lines)-1])
						if err != nil {
							return domain.HookSnapshot{}, err
						}
						if last.EventKind != session.EventTurnFailed && last.EventKind != session.EventTurnCompleted {
							t.Fatal("hook source loaded before reconciliation", last.EventKind)
						}
						return hook.NewLoader().Load(ctx, cwd, home)
					}
					beforeRuns := runner.runs.Load()
					beforeRequests := requests.Load()
					resumed, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, string(thread), false, workspace, chatResourceHooks{interactiveApprovals: true, loadHooks: load, newHookRunner: makeRunner})
					if err != nil {
						t.Fatal(err)
					}
					for _, entry := range resumed.session.HookEntries() {
						if entry.State == "trusted" {
							t.Fatal("restored trust")
						}
					}
					if err := resumed.close(context.Background()); err != nil {
						t.Fatal(err)
					}
					after, err := os.ReadFile(path)
					if err != nil || !bytes.HasPrefix(after, prefix) {
						t.Fatal("recovery rewrote bytes", err)
					}
					if loadCount != 1 || requests.Load() != beforeRequests || runner.runs.Load() != beforeRuns {
						t.Fatal("recovery performed external operation")
					}
					_, plan := loadToolLoopPlan(t, root, thread)
					if plan.ToolRecovery != nil || plan.InterruptedTail != nil {
						t.Fatal("unsealed recovery")
					}
					again, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, string(thread), false, workspace, chatResourceHooks{loadHooks: load, newHookRunner: makeRunner})
					if err != nil {
						t.Fatal(err)
					}
					if err := again.close(context.Background()); err != nil {
						t.Fatal(err)
					}
					repeated, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(after, repeated) {
						t.Fatal("repeated recovery appended duplicate facts", err)
					}
					if checkpoint.EventKind == session.EventHookExecutionStarted {
						for _, line := range bytes.Split(bytes.TrimSuffix(after, []byte("\n")), []byte("\n")) {
							boundary, err := session.DecodeRecord(line)
							if err != nil {
								t.Fatal(err)
							}
							if boundary.Sequence <= checkpoint.Sequence || boundary.BatchIndex+1 != boundary.BatchSize {
								continue
							}
							writeJournalPrefix(t, path, after, boundary.Sequence)
							crashPrefix, err := os.ReadFile(path)
							if err != nil {
								t.Fatal(err)
							}
							second, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, string(thread), false, workspace, chatResourceHooks{loadHooks: load, newHookRunner: makeRunner})
							if err != nil {
								t.Fatal(err)
							}
							if err := second.close(context.Background()); err != nil {
								t.Fatal(err)
							}
							final, err := os.ReadFile(path)
							if err != nil || !bytes.HasPrefix(final, crashPrefix) {
								t.Fatal("second recovery rewrote committed prefix", err)
							}
							_, secondPlan := loadToolLoopPlan(t, root, thread)
							uncertain := false
							if len(secondPlan.Tools) == 1 {
								if event == domain.HookPreToolUse {
									uncertain = secondPlan.Tools[0].Result.Code() == string(domain.HookOutcomeUncertain)
								} else {
									g := secondPlan.Tools[0].PostHooks
									uncertain = g != nil && g.Terminal != nil && g.Terminal.Disposition == domain.PostHookUncertain && (secondPlan.Tools[0].Result.Code() == "ok" && event == domain.HookPostToolUse || secondPlan.Tools[0].Result.Code() == "write_conflict" && event == domain.HookPostToolUseFailure)
								}
							}
							if secondPlan.ToolRecovery != nil || !uncertain {
								t.Fatal("second recovery lost frozen uncertainty")
							}
							if runner.runs.Load() != beforeRuns || requests.Load() != beforeRequests {
								t.Fatal("second crash repeated external action")
							}
						}
					}
					if event == domain.HookPreToolUse && checkpoint.EventKind == session.EventToolCallResult || event.Post() && checkpoint.EventKind == session.EventToolPostHookCompleted || checkpoint.EventKind == session.EventProviderNativeCommit {
						future, err := openChatResources(context.Background(), cfg, root, string(thread), workspace)
						if err != nil {
							t.Fatal(err)
						}
						footprint, err := future.transcriptQueries.projector.(provider.Conversation).HistoryFootprint()
						if err != nil {
							t.Fatal(err)
						}
						submitAppTurn(t, future, "continue after reconciled history")
						if err := future.close(context.Background()); err != nil {
							t.Fatal(err)
						}
						requestMu.Lock()
						next := bytes.Clone(captured[len(captured)-1])
						requestMu.Unlock()
						if compensatedRequest == nil {
							compensatedRequest = next
							compensatedFootprint = footprint
						} else {
							if !bytes.Equal(compensatedRequest, next) || !reflect.DeepEqual(compensatedFootprint, footprint) {
								t.Fatal("result/output crash window changed canonical request or footprint")
							}
							assertRequestFingerprintEqual(t, compensatedRequest, next)
						}
					}

				})
			}
		})
	}
}
