//go:build darwin

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func TestHookBothProvidersRealSandboxAndThreeCapabilities(t *testing.T) {
	for _, name := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "AUTHORIZATION", "COOKIE", "BASH_ENV", "HTTPS_PROXY"} {
		t.Setenv(name, "HOOK-ENV-PRIVATE")
	}
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, capability := range []domain.HookCapability{domain.HookPatch, domain.HookWrite, domain.HookExec} {
			for _, scenario := range []string{"continue-allow", "continue-deny", "exit2", "structured-deny", "timeout"} {
				t.Run(fmt.Sprintf("%s/%s/%s", family, capability, scenario), func(t *testing.T) {
					workspace := t.TempDir()
					if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("old\n"), 0600); err != nil {
						t.Fatal(err)
					}
					command := `cat >/dev/null; test -z "$OPENAI_API_KEY$ANTHROPIC_API_KEY$AUTHORIZATION$COOKIE$BASH_ENV$HTTPS_PROXY" || exit 7; printf '{}'`
					switch scenario {
					case "exit2":
						command = `cat >/dev/null; printf 'HOOK-OUTPUT-PRIVATE'; printf 'Authorization: HOOK-HEADER-PRIVATE' >&2; exit 2`
					case "structured-deny":
						command = `cat >/dev/null; printf '%s' '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"HOOK-REASON-PRIVATE"}}'`
					case "timeout":
						command = `cat >/dev/null; /bin/sleep 5`
					}
					writeHookConfig(t, workspace, command)
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
							serveHookToolSample(w, family, capability)
						} else {
							serveWriteScenarioSample(w, family, n, nil)
						}
					}))
					defer server.Close()
					root := filepath.Join(t.TempDir(), "sessions")
					cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("HOOK-API-PRIVATE")}}, true)
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
					events, err := submitInteractiveFixture(t, control, context.Background(), "perform tool")
					if err != nil {
						t.Fatal(err)
					}
					trusts, approvals, starts, completed := 0, 0, 0, 0
					for e := range events {
						switch e.Kind {
						case protocol.EventHookTrustRequested:
							p, err := protocol.DecodeHookTrustRequested(e)
							if err != nil {
								t.Fatal(err)
							}
							trusts++
							if err := control.ResolveHookTrust(context.Background(), e.TurnID, p, protocol.HookTrustSession); err != nil {
								t.Fatal(err)
							}
						case protocol.EventToolApprovalRequested:
							p, err := protocol.DecodeToolApprovalRequested(e)
							if err != nil {
								t.Fatal(err)
							}
							approvals++
							decision := protocol.ApprovalAllowOnce
							if scenario == "continue-deny" {
								decision = protocol.ApprovalDeny
							}
							if err := control.ResolveApproval(context.Background(), e.TurnID, p, decision, ""); err != nil {
								t.Fatal(err)
							}
						case protocol.EventToolStarted:
							starts++
						case protocol.EventTurnCompleted:
							completed++
						case protocol.EventTurnFailed:
							t.Fatalf("turn failed: %s", e.Payload)
						}
					}
					wantApproval, wantStarts := 0, 0
					if strings.HasPrefix(scenario, "continue") {
						wantApproval = 1
					}
					if scenario == "continue-allow" {
						wantStarts = 1
					}
					if trusts != 1 || approvals != wantApproval || starts != wantStarts || completed != 1 {
						t.Fatal("incorrect authorization/lifecycle", trusts, approvals, starts, completed)
					}
					if err := control.Shutdown(context.Background()); err != nil {
						t.Fatal(err)
					}
					page, err := resources.transcriptQueries.ListTranscript(context.Background(), protocol.TranscriptListQuery{ThreadID: resources.identity.ThreadID, Limit: 50})
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, item := range page.Items {
						if item.Kind == domain.TranscriptCall {
							detail, err := resources.transcriptQueries.ReadTranscriptBody(context.Background(), protocol.TranscriptBodyQuery{ThreadID: resources.identity.ThreadID, Cursor: item.Cursor, Source: protocol.TranscriptResult, Limit: 65536})
							if err != nil || !strings.Contains(detail.Text, "Detailed hook output was not recorded.") {
								t.Fatal("hook detail missing", err, detail.Text)
							}
							found = true
						}
					}
					if !found {
						t.Fatal("missing tool history")
					}
					thread := resources.identity.ThreadID
					path, _ := resources.service.repository.JournalPath(thread)
					if err := resources.close(context.Background()); err != nil {
						t.Fatal(err)
					}
					raw, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					loaded, plan := loadToolLoopPlan(t, root, thread)
					var gates, hookStarts, hookFinishes, results int
					var frozen string
					wantCode := "ok"
					switch scenario {
					case "continue-deny":
						wantCode = "approval_denied"
					case "exit2", "structured-deny":
						wantCode = string(domain.HookDenied)
					case "timeout":
						wantCode = string(domain.HookTimedOut)
					}
					for _, r := range loaded.Records {
						switch r.EventKind {
						case session.EventToolHookGateDecided:
							gates++
						case session.EventHookExecutionStarted:
							hookStarts++
						case session.EventHookExecutionFinished:
							hookFinishes++
						case session.EventToolCallResult:
							p, err := session.DecodeToolCallResultPayload(r)
							if err != nil {
								t.Fatal(err)
							}
							results++
							frozen = p.Preview
							if p.Code != wantCode {
								t.Fatal("wrong result", p.Code, wantCode)
							}
						}
					}
					if gates != 1 || hookStarts != 1 || hookFinishes != 1 || results != 1 || len(plan.Tools) != 1 || len(plan.Tools[0].Hooks) != 1 {
						t.Fatal("invalid durable hook ledger")
					}
					mu.Lock()
					captured := append([][]byte(nil), requests...)
					mu.Unlock()
					if len(captured) != 2 {
						t.Fatal("wrong provider count")
					}
					if !containsHookPreview(captured[1], frozen) {
						t.Fatal("provider continuation lost frozen preview")
					}

					callID := string(plan.Tools[0].Invocation.ProviderCallID())
					wire := "function_call_output"
					if capability == domain.HookPatch {
						wire = "custom_tool_call_output"
					}
					if family == domain.ProviderAnthropic {
						wire = "tool_result"
					}
					if !bytes.Contains(captured[1], []byte(callID)) || !bytes.Contains(captured[1], []byte(wire)) {
						t.Fatal("native output pairing lost")
					}

					for _, b := range append(captured, raw) {
						for _, sentinel := range []string{"HOOK-ENV-PRIVATE", "HOOK-OUTPUT-PRIVATE", "HOOK-HEADER-PRIVATE", "HOOK-REASON-PRIVATE", "HOOK-API-PRIVATE"} {
							if bytes.Contains(b, []byte(sentinel)) {
								t.Fatal("secret escaped", sentinel)
							}
						}
					}
					var first, last struct {
						Tools json.RawMessage `json:"tools"`
					}
					json.Unmarshal(captured[0], &first)
					json.Unmarshal(captured[1], &last)
					var tools []json.RawMessage
					json.Unmarshal(first.Tools, &tools)
					if len(tools) != 9 || !bytes.Equal(first.Tools, last.Tools) {
						t.Fatal("hooks changed stable tools")
					}
					if scenario != "continue-allow" {
						if capability == domain.HookPatch {
							b, _ := os.ReadFile(filepath.Join(workspace, "main.go"))
							if string(b) != "old\n" {
								t.Fatal("denied patch ran")
							}
						} else {
							name := "written"
							if capability == domain.HookExec {
								name = "exec-marker"
							}
							if _, err := os.Stat(filepath.Join(workspace, name)); !os.IsNotExist(err) {
								t.Fatal("denied tool ran")
							}
						}
					}
					if scenario == "continue-allow" && plan.Tools[0].Result.Status() != tool.ResultSuccess {
						t.Fatal("approved target did not run")
					}
				})
			}
		}
	}
}

func TestHookExclusiveBarrierKeepsReadSiblingsAndNativePairing(t *testing.T) {
	testHookExclusiveBarrier(t, domain.HookPreToolUse)
}
func TestPostHookExclusiveBarrierKeepsReadWriteReadExecPairing(t *testing.T) {
	testHookExclusiveBarrier(t, domain.HookPostToolUse)
}
func testHookExclusiveBarrier(t *testing.T, event domain.HookEvent) {
	t.Helper()
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			workspace := t.TempDir()
			if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("unchanged\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if event == domain.HookPostToolUse {
				writePostHookConfig(t, workspace, "cat >/dev/null; exit 2", domain.HookFeedbackNone)
			} else {
				writeHookConfig(t, workspace, "cat >/dev/null; exit 2")
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				n := requests.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				var calls []writeScenarioCall
				if n == 1 {
					calls = []writeScenarioCall{{"Read", `{"file_path":"README.md"}`}, {"Write", `{"file_path":"marker","content":"blocked"}`}, {"Read", `{"file_path":"README.md"}`}}
					if event == domain.HookPostToolUse {
						name := "exec_command"
						if family == domain.ProviderAnthropic {
							name = "Bash"
						}
						calls = append(calls, writeScenarioCall{name, `{"command":"printf executed > exec-marker; exit 7","timeout_ms":1000}`})
					}
				}
				serveWriteScenarioSample(w, family, int(n), calls)
			}))
			defer server.Close()
			root := filepath.Join(t.TempDir(), "sessions")
			cfg := config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("fixture-key")}}
			resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{interactiveApprovals: true})
			if err != nil {
				t.Fatal(err)
			}
			control, err := startTUIControlSession(context.Background(), resources.runtime)
			if err != nil {
				t.Fatal(err)
			}
			events, err := submitInteractiveFixture(t, control, context.Background(), "read, write, read")
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
					if event == domain.HookPreToolUse {
						t.Fatal("blocked tool reached approval")
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
			if err := resources.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			loaded, plan := loadToolLoopPlan(t, root, thread)
			wantTools, wantRuns := 3, 1
			wantMutation := tool.ResultDenied
			if event == domain.HookPostToolUse {
				wantTools, wantRuns, wantMutation = 4, 2, tool.ResultSuccess
			}
			if len(plan.Tools) != wantTools {
				t.Fatal("missing ordered tools")
			}
			var priorResult, hookPlan, blockedResult, laterStart, postTerminal uint64
			var hookRuns int
			for _, record := range loaded.Records {
				switch record.EventKind {
				case session.EventToolCallResult:
					p, _ := session.DecodeToolCallResultPayload(record)
					if p.InvocationID == plan.Tools[0].Invocation.InvocationID() {
						priorResult = record.Sequence
					}
					if p.InvocationID == plan.Tools[1].Invocation.InvocationID() {
						blockedResult = record.Sequence
					}
				case session.EventToolHookGatePlanned:
					hookPlan = record.Sequence
				case session.EventToolPostHookPlanned:
					p, _ := session.DecodeToolPostHookPlanned(record)
					if p.Plan.InvocationID == string(plan.Tools[1].Invocation.InvocationID()) {
						hookPlan = record.Sequence
					}
				case session.EventToolPostHookCompleted:
					p, _ := session.DecodeToolPostHookCompleted(record)
					if p.InvocationID == string(plan.Tools[1].Invocation.InvocationID()) {
						postTerminal = record.Sequence
					}
				case session.EventHookExecutionStarted:
					hookRuns++
				case session.EventToolExecutionStarted:
					p, _ := session.DecodeToolExecutionStartedPayload(record)
					if p.InvocationID == plan.Tools[2].Invocation.InvocationID() {
						laterStart = record.Sequence
					}
				}
			}
			if len(plan.Tools) != wantTools || plan.Tools[0].Result.Status() != tool.ResultSuccess || plan.Tools[1].Result.Status() != wantMutation || plan.Tools[2].Result.Status() != tool.ResultSuccess || hookRuns != wantRuns || !(priorResult < hookPlan && hookPlan < blockedResult && blockedResult < laterStart) {
				t.Fatal("hook crossed exclusive barrier or cancelled sibling")
			}
			if event == domain.HookPostToolUse {
				exit, known := plan.Tools[3].Result.ExecMetadata().ExitCode()
				if !(blockedResult < postTerminal && postTerminal < laterStart) || plan.Tools[3].Result.Status() != tool.ResultSuccess || !known || exit != 7 || plan.Tools[1].PostHooks.Terminal.Code != domain.HookExitFailed || plan.Tools[3].PostHooks.Terminal.Code != domain.HookExitFailed {
					t.Fatal("ordinary post error crossed barrier or rewrote nonzero exec success")
				}
			}
			if requests.Load() != 2 {
				t.Fatal("hook created extra provider tool calls")
			}
		})
	}
}

func TestQuestionHookQuestionKeepsIndependentAuthorizationAndPairing(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			workspace := t.TempDir()
			writeHookConfig(t, workspace, "cat >/dev/null; exit 2")
			name := "request_user_input"
			if family == domain.ProviderAnthropic {
				name = "AskUserQuestion"
			}
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				n := count.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				var calls []writeScenarioCall
				if n == 1 {
					calls = []writeScenarioCall{{name, questionScenarioInput}, {"Write", `{"file_path":"marker","content":"blocked"}`}, {name, questionScenarioInput}}
				}
				serveWriteScenarioSample(w, family, int(n), calls)
			}))
			defer server.Close()
			root := filepath.Join(t.TempDir(), "sessions")
			cfg := config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("fixture-key")}}
			resources, err := openChatResourcesWithHooks(ctx, cfg, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{interactiveApprovals: true})
			if err != nil {
				t.Fatal(err)
			}
			defer resources.close(context.Background())
			control, err := startTUIControlSession(ctx, resources.runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer control.Shutdown(context.Background())
			events, err := submitInteractiveFixture(t, control, ctx, "ask, attempt write, ask again")
			if err != nil {
				t.Fatal(err)
			}
			var trace []string
			resolved := 0
			completed := false
			for e := range events {
				switch e.Kind {
				case protocol.EventUserQuestionRequested:
					trace = append(trace, "question")
					p, err := protocol.DecodeUserQuestionRequested(e)
					if err != nil {
						t.Fatal(err)
					}
					answer, err := tool.NewQuestionAnswerSet([]tool.QuestionAnswer{{QuestionID: "storage", SelectedOptions: []int{0}}})
					if err != nil {
						t.Fatal(err)
					}
					if err := control.ResolveQuestion(ctx, e.TurnID, p, answer); err != nil {
						t.Fatal(err)
					}
				case protocol.EventUserQuestionResolved:
					resolved++
				case protocol.EventHookTrustRequested:
					trace = append(trace, "hook")
					p, err := protocol.DecodeHookTrustRequested(e)
					if err != nil {
						t.Fatal(err)
					}
					if err := control.ResolveHookTrust(ctx, e.TurnID, p, protocol.HookTrustSession); err != nil {
						t.Fatal(err)
					}
				case protocol.EventToolApprovalRequested:
					t.Fatal("hook-blocked write or question reached approval")
				case protocol.EventTurnCompleted:
					completed = true
				case protocol.EventTurnFailed:
					t.Fatal(string(e.Payload))
				}
			}
			if strings.Join(trace, ",") != "question,hook,question" || resolved != 2 || !completed {
				t.Fatal("hook and question routing interfered", trace, resolved)
			}
			if err := control.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			thread := resources.identity.ThreadID
			if err := resources.close(ctx); err != nil {
				t.Fatal(err)
			}
			loaded, plan := loadToolLoopPlan(t, root, thread)
			if len(plan.Tools) != 3 || plan.Tools[0].Result.Status() != tool.ResultSuccess || plan.Tools[1].Result.Code() != string(domain.HookDenied) || plan.Tools[2].Result.Status() != tool.ResultSuccess {
				t.Fatal("question/hook outputs lost original slots")
			}
			plans := 0
			for _, r := range loaded.Records {
				if r.EventKind == session.EventToolApprovalDecided {
					t.Fatal("invented approval fact")
				}
				if r.EventKind == session.EventToolHookGatePlanned {
					p, err := session.DecodeToolHookGatePlanned(r)
					if err != nil || p.InvocationID != string(plan.Tools[1].Invocation.InvocationID()) {
						t.Fatal("question triggered mutation hook", err)
					}
					plans++
				}
			}
			if plans != 1 || count.Load() != 2 {
				t.Fatal("duplicated hook or provider sample")
			}
			if _, err := os.Stat(filepath.Join(workspace, "marker")); !os.IsNotExist(err) {
				t.Fatal("hook-blocked target executed")
			}
		})
	}
}
