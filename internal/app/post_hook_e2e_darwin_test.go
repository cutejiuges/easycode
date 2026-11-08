//go:build darwin

package app

import (
	"bytes"
	"context"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/extension/hook"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool"
	"easycode/internal/tool/builtin"
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
)

type postDisclosureScenario struct {
	name    string
	policy  domain.HookFeedbackPolicy
	command string
	code    domain.HookCode
	body    bool
}

func TestPostReadOnlyDocumentationExampleClassifiesFindingEmptyAndFailure(t *testing.T) {
	for _, name := range []string{"finding", "clean", "missing"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			content := "package main\n"
			if name == "finding" {
				content += "// TODO\n"
			}
			path := filepath.Join(root, "main.go")
			if name != "missing" {
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			workspace, err := builtin.OpenWorkspace(root)
			if err != nil {
				t.Fatal(err)
			}
			defer workspace.Close()
			runner, err := builtin.NewHookRunner(workspace, builtin.OpenCommandEnvironment(), hook.ParseExecution)
			if err != nil || runner.Preflight(context.Background()) != domain.HookOK {
				t.Fatal("read-only sandbox unavailable", err)
			}
			id := "018f1d8a-7b5c-7def-8abc-456789abcdef"
			d, _ := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPostToolUse, domain.HookFeedbackModel, strings.Repeat("a", 64), "example", 7, postReadOnlyExample(), 5)
			plan, _ := domain.NewHookPlan(id, domain.HookExec, []domain.HookReference{d.Reference})
			post, _ := domain.NewPostHookPlan(plan, 256<<10, 16<<10, domain.HookDeliveryAvailable)
			run, _ := domain.NewPostHookRunIdentity(post, 0)
			input, _ := domain.NewHookExecInput("true", "", 1000)
			response, _ := domain.NewHookExecResponse(domain.HookExecMetadata{ExitKnown: true})
			stdin, err := domain.NewPostHookInput(domain.SessionID(id), domain.ThreadID(id), domain.TurnID(id), "call", "exec_command", root, input, response)
			if err != nil {
				t.Fatal(err)
			}
			execution := runner.Run(context.Background(), run, d, stdin, 5*time.Second)
			if execution.Result.Validate() != nil {
				t.Fatal("invalid example execution")
			}
			if name == "missing" {
				if execution.Result.Code != domain.HookExitFailed || execution.Result.ExitCode == 0 || !execution.Feedback.Empty() {
					t.Fatal("example execution failure became model feedback")
				}
			} else {
				if execution.Result.Code != domain.HookOK || execution.Result.ExitCode != 0 || execution.Feedback.Empty() != (name == "clean") {
					t.Fatal("example finding/empty classification changed")
				}
				actual, err := os.ReadFile(path)
				if err != nil || string(actual) != content {
					t.Fatal("example modified workspace", err)
				}
			}
		})
	}
}

func TestPostBothProvidersRealSandboxThreeCapabilitiesAndDisclosure(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, capability := range []domain.HookCapability{domain.HookPatch, domain.HookWrite, domain.HookExec} {
			cases := []postDisclosureScenario{
				{"none", domain.HookFeedbackNone, postReadOnlyExample(), domain.HookFeedbackNotAllowed, false},
				{"model", domain.HookFeedbackModel, postReadOnlyExample(), domain.HookOK, true},
			}
			if capability == domain.HookWrite {
				cases = append(cases,
					postDisclosureScenario{"stderr-model", domain.HookFeedbackModel, "printf 'RAW-HOOK-STDERR-PRIVATE' >&2\n" + postReadOnlyExample(), domain.HookOK, true},
					postDisclosureScenario{"stderr-none", domain.HookFeedbackNone, "printf 'RAW-HOOK-STDERR-PRIVATE' >&2\n" + postReadOnlyExample(), domain.HookFeedbackNotAllowed, false},
					postDisclosureScenario{"invalid-stdout", domain.HookFeedbackModel, "cat >/dev/null; printf 'RAW-HOOK-STDOUT-PRIVATE'; printf 'RAW-HOOK-STDERR-PRIVATE' >&2", domain.HookOutputInvalid, false})
			}
			for _, scenario := range cases {
				policy := scenario.policy
				t.Run(fmt.Sprintf("%s/%s/%s", family, capability, scenario.name), func(t *testing.T) {
					workspace := t.TempDir()
					if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("old\n// TODO\n"), 0600); err != nil {
						t.Fatal(err)
					}
					writePostHookConfig(t, workspace, scenario.command, policy)
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
							serveHookToolSample(&wire, family, capability)
							_, _ = w.Write(withReasoningFixture(family, wire.Bytes()))
						} else {
							serveWriteScenarioSample(w, family, n, nil)
						}
					}))
					defer server.Close()
					root := filepath.Join(t.TempDir(), "sessions")
					cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("fixture-post-key")}}, true)
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
					events, err := submitInteractiveFixture(t, control, context.Background(), "perform post checked tool")
					if err != nil {
						t.Fatal(err)
					}
					trusts, approvals, commits := 0, 0, 0
					for e := range events {
						switch e.Kind {
						case protocol.EventHookTrustRequested:
							p, err := protocol.DecodeHookTrustRequested(e)
							if err != nil || p.Declaration.Reference.Event != domain.HookPostToolUse || approvals != 0 {
								t.Fatal("post trust not before tool approval", err)
							}
							trusts++
							if err := control.ResolveHookTrust(context.Background(), e.TurnID, p, protocol.HookTrustSession); err != nil {
								t.Fatal(err)
							}
						case protocol.EventToolApprovalRequested:
							if trusts != 1 {
								t.Fatal("ordinary approval before post trust")
							}
							approvals++
							p, _ := protocol.DecodeToolApprovalRequested(e)
							if err := control.ResolveApproval(context.Background(), e.TurnID, p, protocol.ApprovalAllowOnce, ""); err != nil {
								t.Fatal(err)
							}
						case protocol.EventPostHookCommitted:
							p, err := protocol.DecodePostHookCommitted(e)
							if err != nil || p.DetailSequence == 0 {
								t.Fatal("invalid durable update", err)
							}
							commits++
						case protocol.EventTurnFailed:
							t.Fatalf("post turn failed: %s", e.Payload)
						}
					}
					if trusts != 1 || approvals != 1 || commits != 3 {
						t.Fatal("wrong authorization/commit sequence", trusts, approvals, commits)
					}
					if err := control.Shutdown(context.Background()); err != nil {
						t.Fatal(err)
					}
					thread := resources.identity.ThreadID
					livePage, err := resources.transcriptQueries.ListTranscript(context.Background(), protocol.TranscriptListQuery{Mode: protocol.TranscriptDetail, ThreadID: thread, Limit: 50})
					if err != nil {
						t.Fatal(err)
					}
					seenReasoning := false
					for _, item := range livePage.Items {
						if item.Kind != domain.TranscriptReasoning {
							continue
						}
						seenReasoning = true
						body, queryErr := resources.transcriptQueries.ReadTranscriptBody(context.Background(), protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: item.Cursor, Source: protocol.TranscriptText, Limit: 64 << 10})
						if queryErr != nil || !strings.Contains(body.Text, "allowed-reasoning-sentinel") || strings.Contains(body.Text, "opaque-reasoning-sentinel") || strings.Contains(body.Text, "raw-reasoning-sentinel") || body.DetailSequence != 0 {
							t.Fatal("reasoning/post detail sources mixed or leaked opaque", queryErr)
						}
					}
					if !seenReasoning {
						t.Fatal("post tool sample lost reasoning projection")
					}
					mainPage, queryErr := resources.transcriptQueries.ListTranscript(context.Background(), protocol.TranscriptListQuery{Mode: protocol.TranscriptMain, ThreadID: thread, Limit: 50})
					if queryErr != nil {
						t.Fatal(queryErr)
					}
					for _, item := range mainPage.Items {
						if item.Kind == domain.TranscriptReasoning {
							t.Fatal("reasoning consumed main transcript slots")
						}
					}
					path, _ := resources.service.repository.JournalPath(thread)
					if err := resources.close(context.Background()); err != nil {
						t.Fatal(err)
					}
					raw, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					loaded, plan := loadToolLoopPlan(t, root, thread)
					if len(plan.Tools) != 1 || !plan.Tools[0].HasResult || plan.Tools[0].Result.Status() != tool.ResultSuccess || plan.Tools[0].PostHooks == nil || plan.Tools[0].PostHooks.Terminal == nil {
						t.Fatal("lost original successful result/post terminal")
					}
					entry := plan.Tools[0]
					post := entry.PostHooks
					want := scenario.code
					if post.Terminal.Code != want || len(post.Finished) != 1 || post.Finished[0].Result.Code != want {
						t.Fatal("wrong disclosure outcome", post.Terminal.Code)
					}
					model, err := tool.NewModelOutput(entry.Result, post.Feedback())
					if err != nil {
						t.Fatal(err)
					}
					mu.Lock()
					captured := append([][]byte(nil), requests...)
					mu.Unlock()
					if len(captured) != 2 || !bytes.Contains(captured[1], []byte("allowed-reasoning-sentinel")) || !bytes.Contains(captured[1], []byte("opaque-reasoning-sentinel")) || !containsHookPreview(captured[1], model.Text()) {
						t.Fatal("provider did not receive frozen composition")
					}
					disclosed := strings.Contains(model.Text(), "Read-only check:")
					if disclosed != scenario.body || bytes.Contains(raw, []byte("Read-only check:")) != disclosed {
						t.Fatal("implicit disclosure or lost authorized content")
					}
					for _, forbidden := range []string{"RAW-HOOK-STDERR-PRIVATE", "RAW-HOOK-STDOUT-PRIVATE"} {
						if bytes.Contains(raw, []byte(forbidden)) || bytes.Contains(bytes.Join(captured, nil), []byte(forbidden)) {
							t.Fatal("raw hook stream disclosed", forbidden)
						}
					}
					resultSeq, postSeq, results := uint64(0), uint64(0), 0
					for _, record := range loaded.Records {
						if record.EventKind == session.EventToolCallResult {
							resultSeq = record.Sequence
							results++
						}
						if record.EventKind == session.EventHookExecutionStarted {
							postSeq = record.Sequence
						}
					}
					if results != 1 || postSeq <= resultSeq {
						t.Fatal("post effect preceded unique durable result")
					}
					// 原生恢复必须复用同一正文与缓存事实，不能读取配置或自动重跑工具。
					beforeRequests := len(captured)
					resumed, err := openChatResources(context.Background(), cfg, root, string(thread), workspace)
					if err != nil {
						t.Fatal(err)
					}
					restoredPage, err := resumed.transcriptQueries.ListTranscript(context.Background(), protocol.TranscriptListQuery{Mode: protocol.TranscriptDetail, ThreadID: thread, Limit: 50})
					if err != nil || len(restoredPage.Items) != len(livePage.Items) {
						t.Fatal("restored post list differs", err)
					}
					found := false
					for i, item := range restoredPage.Items {
						if item.Kind == domain.TranscriptReasoning {
							previous := livePage.Items[i]
							if item.Reasoning == nil || previous.Reasoning == nil || *item.Reasoning != *previous.Reasoning || item.Text != previous.Text || item.Cursor != previous.Cursor {
								t.Fatal("restored reasoning changed alongside post feedback")
							}
						}
						if item.Kind == domain.TranscriptCall {
							found = true
							if item.PostHook.State != protocol.HookActivityCompleted || item.PostHook.Code != want || item.Activity.Status != "success" || item.PostHook != livePage.Items[i].PostHook || item.Cursor != livePage.Items[i].Cursor || item.Preview != livePage.Items[i].Preview {
								t.Fatal("native commit/restore lost post summary or tool position")
							}
						}
					}
					if !found {
						t.Fatal("restored tool missing")
					}
					if err := resumed.close(context.Background()); err != nil {
						t.Fatal(err)
					}
					mu.Lock()
					afterRequests := len(requests)
					mu.Unlock()
					again, _ := os.ReadFile(path)
					if beforeRequests != afterRequests || !bytes.Equal(raw, again) {
						t.Fatal("resume changed facts or sampled")
					}
				})
			}
		}
	}
}
