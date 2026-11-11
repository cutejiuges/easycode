//go:build darwin

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
	"sync"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func writeBothPostBranchRegressionConfig(t *testing.T, workspace string) {
	t.Helper()
	group := func(event domain.HookEvent, id, body string) string {
		payload := fmt.Sprintf(`{"hookSpecificOutput":{"hookEventName":"%s","additionalContext":"%s"}}`, event, body)
		command, err := codec.MarshalStable("cat >/dev/null; printf '%s' '" + payload + "'")
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf(`[{"matcher":"Bash|exec_command","hooks":[{"id":"%s","type":"command","command":%s,"feedback":"model","timeout":5}]}]`, id, command)
	}
	content := fmt.Sprintf(`{"hooks":{"PostToolUse":%s,"PostToolUseFailure":%s}}`, group(domain.HookPostToolUse, "success-check", "SUCCESS-BRANCH-DIAGNOSIS"), group(domain.HookPostToolUseFailure, "failure-check", "FAILURE-BRANCH-MUST-NOT-RUN"))
	dir := filepath.Join(workspace, ".easycode")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPostBothBranchesRealExecNormalExitsKeepSuccessForBothProviders(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, tc := range []struct {
			name, command string
			exit          int
		}{{"zero", "exit 0", 0}, {"one", "exit 1", 1}, {"grep-no-match", "/usr/bin/grep -q NEVER /dev/null", 1}} {
			t.Run(string(family)+"/"+tc.name, func(t *testing.T) {
				workspace := t.TempDir()
				writeBothPostBranchRegressionConfig(t, workspace)
				var mu sync.Mutex
				var requests [][]byte
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						return
					}
					mu.Lock()
					requests = append(requests, body)
					n := len(requests)
					mu.Unlock()
					w.Header().Set("Content-Type", "text/event-stream")
					if n == 1 {
						writeExecCommandSample(w, family, "printf x >> exec-invoked; printf ORIGINAL-STDOUT; "+tc.command, 1000)
					} else {
						serveWriteScenarioSample(w, family, n, nil)
					}
				}))
				defer server.Close()
				cfg := config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("synthetic-regression-key")}}
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
				events, err := submitInteractiveFixture(t, control, context.Background(), "check exact post branch")
				if err != nil {
					t.Fatal(err)
				}
				trusts, approvals, completed := 0, 0, 0
				for e := range events {
					switch e.Kind {
					case protocol.EventHookTrustRequested:
						p, err := protocol.DecodeHookTrustRequested(e)
						if err != nil {
							t.Fatal(err)
						}
						expected := domain.HookPostToolUse
						if trusts == 1 {
							expected = domain.HookPostToolUseFailure
						}
						if approvals != 0 || trusts >= 2 || p.Declaration.Reference.Event != expected {
							t.Fatal("branch trust not exact or before approval")
						}
						trusts++
						if err := control.ResolveHookTrust(context.Background(), e.TurnID, p, protocol.HookTrustSession); err != nil {
							t.Fatal(err)
						}
					case protocol.EventToolApprovalRequested:
						if trusts != 2 {
							t.Fatal("ordinary approval preceded both branch reviews")
						}
						approvals++
						p, err := protocol.DecodeToolApprovalRequested(e)
						if err != nil {
							t.Fatal(err)
						}
						if err := control.ResolveApproval(context.Background(), e.TurnID, p, protocol.ApprovalAllowOnce, ""); err != nil {
							t.Fatal(err)
						}
					case protocol.EventTurnCompleted:
						completed++
					case protocol.EventTurnFailed:
						t.Fatalf("normal exit broke post flow: %s", e.Payload)
					}
				}
				if err := control.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
				thread := resources.identity.ThreadID
				if err := resources.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				marker, err := os.ReadFile(filepath.Join(workspace, "exec-invoked"))
				if err != nil || string(marker) != "x" {
					t.Fatal("original exec did not run exactly once", err)
				}
				loaded, plan := loadToolLoopPlan(t, root, thread)
				if len(plan.Tools) != 1 || !plan.Tools[0].HasResult || plan.ToolRecovery != nil || plan.InterruptedTail != nil {
					t.Fatal("original output pairing or sealed turn lost")
				}
				entry := plan.Tools[0]
				g := entry.PostHooks
				exit, known := entry.Result.ExecMetadata().ExitCode()
				if entry.Result.Status() != tool.ResultSuccess || entry.Result.Code() != "ok" || !known || exit != tc.exit || g == nil || g.Terminal == nil {
					t.Fatal("normal nonzero exit reclassified")
				}
				if len(g.Plan.Branches) != 2 || g.Plan.Reservation != 16<<10 || entry.Result.Budget().Allowance() != 240<<10 || g.Terminal.Route != domain.PostHookRouteSuccess || g.Terminal.Code != domain.HookOK || len(g.Finished) != 1 || g.Finished[0].Result.Identity.Event != domain.HookPostToolUse {
					t.Fatal("alternate branches ran together or duplicated budget")
				}
				starts, results, postStarts, postFinishes, terminals := 0, 0, 0, 0, 0
				var admission tool.ExecutionAdmission
				for _, record := range loaded.Records {
					switch record.EventKind {
					case session.EventToolExecutionStarted:
						starts++
						admission, err = tool.NewExecutionAdmission(entry.Invocation.InvocationID(), record.Sequence)
						if err != nil {
							t.Fatal(err)
						}
					case session.EventToolCallResult:
						results++
					case session.EventHookExecutionStarted:
						postStarts++
					case session.EventHookExecutionFinished:
						postFinishes++
					case session.EventToolPostHookCompleted:
						terminals++
					}
				}
				if starts != 1 || results != 1 || postStarts != 1 || postFinishes != 1 || terminals != 1 || trusts != 2 || approvals != 1 || completed != 1 {
					t.Fatal("duplicate effect, result or check lifecycle")
				}
				selection, err := tool.NewPostHookSelection(g.Plan, admission, entry.Result)
				if err != nil {
					t.Fatal(err)
				}
				output, err := tool.NewPostModelOutput(entry.Result, selection, g.Feedback())
				if err != nil {
					t.Fatal(err)
				}
				mu.Lock()
				captured := append([][]byte(nil), requests...)
				mu.Unlock()
				if len(captured) != 2 || !containsHookPreview(captured[1], output.Text()) || !bytes.Contains(captured[1], []byte("SUCCESS-BRANCH-DIAGNOSIS")) || bytes.Contains(bytes.Join(captured, nil), []byte("FAILURE-BRANCH-MUST-NOT-RUN")) {
					t.Fatal("provider got wrong selected feedback or extra sample")
				}
			})
		}
	}
}
