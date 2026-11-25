//go:build darwin

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
					writeAppTestFile(t, filepath.Join(workspace, "main.go"), []byte("old\n"), 0600)
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
					var recorder requestRecorder
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requestIndex, _ := recorder.record(t, r)
						n := requestIndex + 1
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
					resources := mustOpenChatResourcesWithHooks(t, context.Background(), cfg, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{interactiveApprovals: true})
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
					page, err := resources.transcriptQueries.ListTranscript(context.Background(), protocol.TranscriptListQuery{Mode: protocol.TranscriptDetail, ThreadID: resources.identity.ThreadID, Limit: 50})
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
					closeAppTestResources(t, resources)
					raw := readAppTestFile(t, path)
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
					captured := recorder.snapshot()
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
					if len(tools) != 10 || !bytes.Equal(first.Tools, last.Tools) {
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
