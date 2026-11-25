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
	rewrite  bool
}

func (*appHookRunner) Preflight(context.Context) domain.HookCode { return domain.HookOK }
func (r *appHookRunner) Run(_ context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, input domain.HookInput, _ time.Duration) domain.HookExecution {
	r.runs.Add(1)
	candidate := domain.HookFeedbackCandidate{}
	if r.feedback && id.Event.Post() {
		candidate, _ = domain.NewHookFeedbackCandidate("explicit check feedback")
	}
	var replacement domain.HookInputReplacement
	if r.rewrite && id.Event == domain.HookPreToolUse {
		next, _ := domain.NewHookWriteInput("effective-marker", "changed")
		replacement, _ = domain.NewHookInputReplacement(next, input.RewriteContext().Profile())
	}
	return domain.HookExecution{Replacement: replacement, Result: domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}, Feedback: candidate}
}
func TestHookResumeReconcilesEveryBatchBeforeLoadingNewSnapshot(t *testing.T) {
	testHookResumeReconcilesEveryBatch(t, domain.HookPreToolUse, false)
}
func testHookResumeReconcilesEveryBatch(t *testing.T, event domain.HookEvent, rewrite bool) {
	t.Helper()
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			// 两家 Provider 各自持有完整环境；同一家内部的崩溃检查点仍按顺序恢复。
			t.Parallel()
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
			writeAppTestFile(t, file, b, 0600)
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
			runner := &appHookRunner{feedback: event.Post(), rewrite: rewrite}
			makeRunner := func(*builtin.Workspace, builtin.CommandEnvironment, builtin.HookOutputParser) (chatRuntime.HookRunner, error) {
				return runner, nil
			}
			resources := mustOpenChatResourcesWithHooks(t, context.Background(), cfg, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{interactiveApprovals: true, newHookRunner: makeRunner})
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
						writeAppTestFile(t, filepath.Join(workspace, "marker"), []byte("external change"), 0600)
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
			closeAppTestResources(t, resources)
			if rewrite {
				if _, err := os.Stat(filepath.Join(workspace, "marker")); !os.IsNotExist(err) {
					t.Fatal("original write happened", err)
				}
				b, err := os.ReadFile(filepath.Join(workspace, "effective-marker"))
				if err != nil || string(b) != "changed" {
					t.Fatal("effective write missing", err)
				}
			}
			baseline := readAppTestFile(t, path)
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
					prefix := readAppTestFile(t, path)
					// 新来源损坏和缺失都不能阻止旧计划先收口，也不能触发旧脚本重跑。
					if checkpoint.Sequence%2 == 0 {
						writeAppTestFile(t, file, []byte(`{"invalid":true}`), 0600)
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
					resumed := mustOpenChatResourcesWithHooks(t, context.Background(), cfg, dataPaths{sessionRoot: root}, string(thread), false, workspace, chatResourceHooks{interactiveApprovals: true, loadHooks: load, newHookRunner: makeRunner})
					for _, entry := range resumed.session.HookEntries() {
						if entry.State == "trusted" {
							t.Fatal("restored trust")
						}
					}
					closeAppTestResources(t, resumed)
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
					again := mustOpenChatResourcesWithHooks(t, context.Background(), cfg, dataPaths{sessionRoot: root}, string(thread), false, workspace, chatResourceHooks{loadHooks: load, newHookRunner: makeRunner})
					closeAppTestResources(t, again)
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
							crashPrefix := readAppTestFile(t, path)
							second := mustOpenChatResourcesWithHooks(t, context.Background(), cfg, dataPaths{sessionRoot: root}, string(thread), false, workspace, chatResourceHooks{loadHooks: load, newHookRunner: makeRunner})
							closeAppTestResources(t, second)
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
						// 新 turn 的完成检查会拒绝坏来源；此处仅比较旧恢复生成的请求，先移除测试故障来源。
						if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
							t.Fatal(err)
						}
						future := mustOpenChatResources(t, context.Background(), cfg, root, string(thread), workspace)
						_, footprint, err := future.transcriptQueries.projector.(provider.Conversation).ProjectContextWindow()
						if err != nil {
							t.Fatal(err)
						}
						submitAppTurn(t, future, "continue after reconciled history")
						closeAppTestResources(t, future)
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
