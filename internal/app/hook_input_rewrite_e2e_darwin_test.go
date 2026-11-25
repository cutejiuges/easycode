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
	"easycode/internal/tool/builtin"
)

func writeInputRewriteConfig(t *testing.T, workspace string, input domain.HookToolInput, withPost bool) {
	t.Helper()
	canonical, err := input.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	output := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":` + string(canonical) + `}}`
	command := `cat >/dev/null; printf '%s' '` + strings.ReplaceAll(output, "'", "'\"'\"'") + `'; printf 'RAW-REWRITE-PRIVATE' >&2`
	dir := filepath.Join(workspace, ".easycode")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(command)
	post := ""
	if withPost {
		post = `,"PostToolUse":[{"matcher":"Edit|apply_patch|Write|Bash|exec_command","hooks":[{"id":"post","type":"command","feedback":"model","command":"cat >/dev/null; printf '%s' '{\"hookSpecificOutput\":{\"hookEventName\":\"PostToolUse\",\"additionalContext\":\"post rewrite check\"}}'"}]}]`
	}
	if withPost {
		post += `,"PostToolUseFailure":[{"matcher":"Edit|apply_patch|Write|Bash|exec_command","hooks":[{"id":"failure","type":"command","feedback":"model","command":"cat >/dev/null; printf '%s' '{\"hookSpecificOutput\":{\"hookEventName\":\"PostToolUseFailure\",\"additionalContext\":\"post rewrite failure\"}}'"}]}]`
	}
	config := fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":"Edit|apply_patch|Write|Bash|exec_command","hooks":[{"id":"rewrite","type":"command","command":%s}]}]%s}}`, b, post)
	writeAppTestFile(t, filepath.Join(dir, "hooks.json"), []byte(config), 0600)
}

func TestInputRewriteBothProvidersRealEffectsAndOfflineRestore(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, capability := range []domain.HookCapability{domain.HookPatch, domain.HookWrite, domain.HookExec} {
			for _, mode := range []string{"plain", "post", "deny", "missing-read", "symlink-swap", "base-conflict", "spawn-failure", "artifact"} {
				if (mode == "missing-read" || mode == "symlink-swap" || mode == "base-conflict") && capability != domain.HookWrite || (mode == "spawn-failure" || mode == "artifact") && capability != domain.HookExec {
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/%s", family, capability, mode), func(t *testing.T) {
					workspace, root := t.TempDir(), filepath.Join(t.TempDir(), "sessions")
					for _, path := range []string{"main.go", "effective.txt"} {
						writeAppTestFile(t, filepath.Join(workspace, path), []byte("old\n"), 0600)
					}
					var input domain.HookToolInput
					switch capability {
					case domain.HookPatch:
						line := family == domain.ProviderOpenAI
						old, next := "old", "effective"
						if line {
							old += "\n"
							next += "\n"
						}
						input, _ = domain.NewHookPatchInput("effective.txt", []domain.HookReplacement{{OldText: old, NewText: next, LineBased: line}})
					case domain.HookWrite:
						input, _ = domain.NewHookWriteInput("effective.txt", "effective\n")
					case domain.HookExec:
						input, _ = domain.NewHookExecInput("printf effective > rewritten-marker; exit 7", "", 1000)
					}
					if mode == "artifact" {
						input, _ = domain.NewHookExecInput(`printf effective > rewritten-marker; /usr/bin/head -c 600000 /dev/zero | /usr/bin/tr '\000' x; exit 7`, "", 5000)
					}
					outside := filepath.Join(t.TempDir(), "protected")
					writeAppTestFile(t, outside, []byte("protected\n"), 0600)
					writeInputRewriteConfig(t, workspace, input, mode == "post" || mode == "spawn-failure")
					var recorder requestRecorder
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requestIndex, _ := recorder.record(t, r)
						n := requestIndex + 1
						w.Header().Set("Content-Type", "text/event-stream")
						switch n {
						case 1:
							path := "effective.txt"
							if mode == "missing-read" {
								path = "main.go"
							}
							serveWriteScenarioSample(w, family, 1, []writeScenarioCall{{"Read", `{"file_path":"` + path + `"}`}})
						case 2:
							var wire bytes.Buffer
							if capability == domain.HookWrite {
								serveWriteScenarioSample(&wire, family, 2, []writeScenarioCall{{"Write", `{"file_path":"written","content":"new"}`}})
							} else {
								serveHookToolSample(&wire, family, capability)
							}
							_, _ = w.Write(withReasoningFixture(family, wire.Bytes()))
						default:
							serveWriteScenarioSample(w, family, n, nil)
						}
					}))
					defer server.Close()
					cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("rewrite-fixture-key")}}, true)
					hooks := chatResourceHooks{interactiveApprovals: true}
					startupFailure := &spawnFailureExec{}
					if mode == "spawn-failure" {
						hooks.newExecExecutor = func(*builtin.Workspace, builtin.CommandEnvironment, tool.ExecOutputRenderer) (tool.ExecExecutor, error) {
							return startupFailure, nil
						}
					}
					resources := mustOpenChatResourcesWithHooks(t, context.Background(), cfg, dataPaths{sessionRoot: root}, "", false, workspace, hooks)
					defer resources.close(context.Background())
					control, err := startTUIControlSession(context.Background(), resources.runtime)
					if err != nil {
						t.Fatal(err)
					}
					defer control.Shutdown(context.Background())
					events, err := submitInteractiveFixture(t, control, context.Background(), "read then use rewritten input")
					if err != nil {
						t.Fatal(err)
					}
					approvals, starts := 0, 0
					for e := range events {
						switch e.Kind {
						case protocol.EventHookTrustRequested:
							p, err := protocol.DecodeHookTrustRequested(e)
							if err != nil {
								t.Fatal(err)
							}
							if err := control.ResolveHookTrust(context.Background(), e.TurnID, p, protocol.HookTrustSession); err != nil {
								t.Fatal(err)
							}
						case protocol.EventToolApprovalRequested:
							approvals++
							p, err := protocol.DecodeToolApprovalRequested(e)
							if err != nil || p.InputRewrite.Kind() != protocol.PreInputSelected || !strings.Contains(p.Reason, "project:rewrite") {
								t.Fatal("missing effective source review", err)
							}
							if capability == domain.HookExec {
								command, _, _, _ := input.Exec()
								if p.Command != command {
									t.Fatal("original command approved")
								}
							} else if p.RelativePath != "effective.txt" {
								t.Fatal("original target approved")
							}
							if mode == "symlink-swap" {
								if err := os.Remove(filepath.Join(workspace, "effective.txt")); err != nil {
									t.Fatal(err)
								}
								if err := os.Symlink(outside, filepath.Join(workspace, "effective.txt")); err != nil {
									t.Fatal(err)
								}
							}
							if mode == "base-conflict" {
								writeAppTestFile(t, filepath.Join(workspace, "effective.txt"), []byte("concurrent\n"), 0600)
							}
							decision := protocol.ApprovalAllowOnce
							if mode == "deny" {
								decision = protocol.ApprovalDeny
							}
							if err := control.ResolveApproval(context.Background(), e.TurnID, p, decision, ""); err != nil {
								t.Fatal(err)
							}
						case protocol.EventToolStarted:
							p, err := protocol.DecodeToolActivity(e)
							if err != nil {
								t.Fatal(err)
							}
							if p.Tool != protocol.ToolRead {
								starts++
							}
						case protocol.EventTurnFailed:
							t.Fatalf("rewrite turn failed: %s", e.Payload)
						}
					}
					if err := control.Shutdown(context.Background()); err != nil {
						t.Fatal(err)
					}
					thread := resources.identity.ThreadID
					live, err := resources.transcriptQueries.ListTranscript(context.Background(), protocol.TranscriptListQuery{Mode: protocol.TranscriptDetail, ThreadID: thread, Limit: 50})
					if err != nil {
						t.Fatal(err)
					}
					closeAppTestResources(t, resources)
					_, plan := loadToolLoopPlan(t, root, thread)
					if len(plan.Tools) != 2 {
						t.Fatal("missing ordered read/mutation")
					}
					entry := plan.Tools[1]
					evidence, err := entry.InputResultEvidence()
					if err != nil || !evidence.Selection().Changed() {
						t.Fatal("lost selected execution evidence", err)
					}
					if got, _ := tool.HookInputForInvocation(entry.Invocation); got.Equal(input) {
						t.Fatal("native original was overwritten")
					}
					executed := mode != "deny" && mode != "missing-read"
					if executed {
						failed := mode == "spawn-failure" || mode == "symlink-swap" || mode == "base-conflict"
						if starts != 1 || approvals != 1 || (entry.Result.Status() == tool.ResultSuccess) == failed || !strings.Contains(entry.Result.Preview().Text(), "Admitted tool attempt") {
							t.Fatal("effective input did not execute", starts, approvals, entry.Result.Code())
						}
						if capability == domain.HookExec && mode != "spawn-failure" {
							if exit, known := entry.Result.ExecMetadata().ExitCode(); !known || exit != 7 {
								t.Fatal("normal nonzero misrouted")
							}
						}
					} else if starts != 0 || !strings.Contains(entry.Result.Preview().Text(), "tool was not executed") {
						t.Fatal("unadmitted result claimed effects")
					}
					if mode == "missing-read" && (approvals != 0 || entry.Result.Code() != "write_requires_full_read") {
						t.Fatal("original read authorized changed target", entry.Result.Code())
					}
					original, err := os.ReadFile(filepath.Join(workspace, "main.go"))
					if err != nil || string(original) != "old\n" {
						t.Fatal("original patch target modified", err)
					}
					for _, originalMarker := range []string{"written", "exec-marker"} {
						if _, err := os.Stat(filepath.Join(workspace, originalMarker)); !os.IsNotExist(err) {
							t.Fatal("original side effect happened", originalMarker, err)
						}
					}
					mutated := executed && mode != "spawn-failure"
					if capability == domain.HookExec {
						b, err := os.ReadFile(filepath.Join(workspace, "rewritten-marker"))
						if mutated && (err != nil || string(b) != "effective") || !mutated && !os.IsNotExist(err) {
							t.Fatal("effective command marker mismatch", err)
						}
					} else {
						b, err := os.ReadFile(filepath.Join(workspace, "effective.txt"))
						want := "old\n"
						if executed {
							want = "effective\n"
						}
						if mode == "base-conflict" {
							want = "concurrent\n"
						}
						if mode == "symlink-swap" {
							want = "protected\n"
						}
						if err != nil || string(b) != want {
							t.Fatal("effective file mismatch", err, string(b))
						}
					}
					if b, err := os.ReadFile(outside); err != nil || string(b) != "protected\n" {
						t.Fatal("rewritten target changed outside bytes", err)
					}
					if mode == "spawn-failure" && (startupFailure.calls != 1 || entry.Result.Code() != "process_start_failed") {
						t.Fatal("lost admitted startup failure")
					}
					captured := recorder.snapshot()
					if len(captured) != 3 || !containsHookPreview(captured[2], entry.Result.Preview().Text()+postEntryText(t, entry)) || bytes.Contains(bytes.Join(captured, nil), []byte("RAW-REWRITE-PRIVATE")) {
						t.Fatal("wrong provider disclosure/pairing")
					}
					if err := os.RemoveAll(filepath.Join(workspace, ".easycode")); err != nil {
						t.Fatal(err)
					}
					resumed, err := openChatResources(context.Background(), cfg, root, string(thread), workspace)
					if err != nil {
						t.Fatal("offline restore failed", err)
					}
					defer resumed.close(context.Background())
					restored, err := resumed.transcriptQueries.ListTranscript(context.Background(), protocol.TranscriptListQuery{Mode: protocol.TranscriptDetail, ThreadID: thread, Limit: 50})
					if err != nil {
						t.Fatal(err)
					}
					first, _ := json.Marshal(live)
					second, _ := json.Marshal(restored)
					if !bytes.Equal(first, second) {
						t.Fatalf("live/restored projection changed\n%s\n%s", first, second)
					}
					for _, item := range restored.Items {
						if item.PreHook != nil {
							page, err := resumed.transcriptQueries.ReadTranscriptBody(context.Background(), protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: item.Cursor, Source: protocol.TranscriptPreHooks, DetailSequence: item.PreHook.PreInput.Sequence(), Limit: 64 << 10})
							if err != nil || !strings.Contains(page.Text, "original request O") || !strings.Contains(page.Text, "effective input E") || !strings.Contains(page.Text, "project:rewrite") {
								t.Fatal("lost immutable input audit", err)
							}
						}
					}
					if mode == "artifact" {
						metadata := entry.Result.ExecMetadata()
						if metadata.Stdout().ObservedBytes() != 600000 || !metadata.OutputTruncated() {
							t.Fatal("large output was not captured")
						}
						var raw strings.Builder
						for offset := uint64(0); offset < 600000; {
							page, err := resumed.transcriptQueries.ReadToolOutput(context.Background(), protocol.ToolOutputQuery{ThreadID: thread, InvocationID: string(entry.Invocation.InvocationID()), Stream: protocol.ToolStdout, Offset: offset, Limit: 64 << 10})
							if err != nil || page.NextOffset <= offset {
								t.Fatal("artifact unavailable after input rewrite restore", err)
							}
							raw.WriteString(page.Text)
							offset = page.NextOffset
						}
						if raw.String() != strings.Repeat("x", 600000) || entry.Result.Budget().ActualBytes() > entry.Result.Budget().Allowance() {
							t.Fatal("input note leaked into raw artifact or budget")
						}
					}
					after := len(recorder.snapshot())
					if after != 3 {
						t.Fatal("restore performed network/tool turn")
					}
				})
			}
		}
	}
}

func postEntryText(t *testing.T, entry session.ToolHistoryEntry) string {
	t.Helper()
	if entry.PostHooks == nil {
		return ""
	}
	text, err := tool.RenderPostFeedback(entry.PostHooks.Feedback())
	if err != nil {
		t.Fatal(err)
	}
	return text
}
