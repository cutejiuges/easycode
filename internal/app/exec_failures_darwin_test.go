//go:build darwin

package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool"
	"easycode/internal/tool/builtin"
	"easycode/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

type spawnFailureExec struct{ calls int }

func (executor *spawnFailureExec) Execute(_ context.Context, candidate tool.ExecCandidate, _ func(tool.ExecProgress)) tool.InvocationResult {
	executor.calls++
	return tool.NewExecErrorResult(candidate.Invocation(), tool.ResultError, "process_start_failed", "Exec failed: process_start_failed")
}

func TestExecFailurePathsEndToEndForBothProviders(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, scenario := range []string{"deny", "abort", "interrupt", "timeout", "sandbox-denial", "spawn-failure", "large-output"} {
			t.Run(string(family)+"/"+scenario, func(t *testing.T) {
				workspace, root := t.TempDir(), filepath.Join(t.TempDir(), "sessions")
				outside := filepath.Join(t.TempDir(), "protected")
				if err := os.WriteFile(outside, []byte("unchanged"), 0600); err != nil {
					t.Fatal(err)
				}
				command := "printf forbidden > exec-marker"
				wantCode, decision, accepted := "ok", protocol.ApprovalAllowOnce, true
				wantRequests := int32(2)
				switch scenario {
				case "deny":
					wantCode, decision, accepted = "approval_denied", protocol.ApprovalDeny, false
				case "abort":
					wantCode, decision, accepted, wantRequests = "user_aborted", protocol.ApprovalAbort, false, 1
				case "interrupt":
					command, wantCode, wantRequests = "printf ready-to-interrupt; exec /bin/sleep 30", "cancelled", 1
				case "timeout":
					command, wantCode = "printf ready-to-timeout; exec /bin/sleep 30", "timed_out"
				case "sandbox-denial":
					command = "printf forbidden > " + strconv.Quote(outside)
				case "spawn-failure":
					wantCode = "process_start_failed"
				case "large-output":
					command = `/usr/bin/head -c 600000 /dev/zero | /usr/bin/tr '\000' x`
				}
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					_, _ = io.Copy(io.Discard, request.Body)
					writer.Header().Set("Content-Type", "text/event-stream")
					if requests.Add(1) == 1 {
						writeExecCommandSample(writer, family, command, 1000)
					} else {
						writeExecSample(writer, family, 2)
					}
				}))
				defer server.Close()
				hooks := chatResourceHooks{interactiveApprovals: true}
				failedExecutor := &spawnFailureExec{}
				if scenario == "spawn-failure" {
					// 平台启动错误通过执行边界注入；真实launcher失败另由builtin集成测试验证。
					hooks.newExecExecutor = func(*builtin.Workspace, builtin.CommandEnvironment, tool.ExecOutputRenderer) (tool.ExecExecutor, error) {
						return failedExecutor, nil
					}
				}
				resources, err := openChatResourcesWithHooks(context.Background(), config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("fixture-private-key")}}, dataPaths{sessionRoot: root}, "", false, workspace, hooks)
				if err != nil {
					t.Fatal(err)
				}
				defer resources.close(context.Background())
				control, err := startTUIControlSession(context.Background(), resources.runtime)
				if err != nil {
					t.Fatal(err)
				}
				defer control.stop(context.Background())
				events, err := control.Submit(context.Background(), "run local check")
				if err != nil {
					t.Fatal(err)
				}
				var ui tea.Model = tui.NewModel(context.Background(), "test", control)
				ui, _ = ui.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
				ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("run local check")})
				ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
				starts, terminals, turns := 0, 0, 0
				interrupted := false
				for event := range events {
					ui, _ = ui.Update(tui.RuntimeEventMsg{Event: event})
					switch event.Kind {
					case protocol.EventToolApprovalRequested:
						payload, err := protocol.DecodeToolApprovalRequested(event)
						if err != nil {
							t.Fatal(err)
						}
						if err := control.ResolveApproval(context.Background(), event.TurnID, payload, decision, ""); err != nil {
							t.Fatal(err)
						}
					case protocol.EventCommandStarted:
						starts++
					case protocol.EventCommandProgress:
						payload, err := protocol.DecodeCommandActivity(event)
						if err != nil {
							t.Fatal(err)
						}
						if scenario == "interrupt" && !interrupted && strings.Contains(payload.Recent, "ready-to-interrupt") {
							control.Interrupt()
							interrupted = true
						}
					case protocol.EventCommandTerminal:
						terminals++
						payload, err := protocol.DecodeCommandTerminal(event)
						if err != nil || payload.Code != wantCode {
							t.Fatalf("terminal code = %s: %v", payload.Code, err)
						}
					case protocol.EventTurnCompleted, protocol.EventTurnFailed:
						turns++
					}
				}
				if err := control.stop(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := resources.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				wantActivities := 0
				if accepted {
					wantActivities = 1
				}
				if starts != wantActivities || terminals != wantActivities || turns != 1 || requests.Load() != wantRequests {
					t.Fatalf("lifecycle = %d/%d/%d requests=%d", starts, terminals, turns, requests.Load())
				}
				if scenario == "interrupt" && !interrupted {
					t.Fatal("interrupt did not reach running command")
				}
				if scenario == "spawn-failure" && failedExecutor.calls != 1 {
					t.Fatal("spawn failure retried")
				}
				loaded, plan := loadToolLoopPlan(t, root, resources.identity.ThreadID)
				if plan.ToolRecovery != nil || plan.InterruptedTail != nil {
					t.Fatal("terminal left unsealed command")
				}
				results := 0
				for _, record := range loaded.Records {
					if record.EventKind != session.EventToolCallResult {
						continue
					}
					payload, err := session.DecodeToolCallResultPayload(record)
					if err != nil || payload.Code != wantCode {
						t.Fatalf("durable result = %s: %v", payload.Code, err)
					}
					results++
					if scenario == "sandbox-denial" && (!payload.ExecMetadata.ExitKnown || payload.ExecMetadata.ExitCode == 0) {
						t.Fatal("sandbox allowed outside write")
					}
					if scenario == "large-output" && (!payload.ExecMetadata.Truncated || payload.ExecMetadata.StdoutObserved != 600000 || len(payload.Preview) > tool.ExecModelPreviewBytes) {
						t.Fatal("large output exceeded result budget")
					}
				}
				if results != 1 {
					t.Fatal("duplicate or missing durable result")
				}
				if len(plan.NativeCommits) < 2 || !bytes.Contains(plan.NativeCommits[1].Commit.Payload, []byte("call-exec")) {
					t.Fatal("failure lacks paired native output")
				}
				if _, err := os.Stat(filepath.Join(workspace, "exec-marker")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("failed command wrote forbidden marker")
				}
				content, err := os.ReadFile(outside)
				if err != nil || string(content) != "unchanged" {
					t.Fatal("sandbox changed protected file")
				}
				if !strings.Contains(ui.View(), "Status: idle") {
					t.Fatal("TUI stayed busy after cleanup")
				}
			})
		}
	}
}

func TestExecConstructionFailureClosesWorkspaceBeforeReturning(t *testing.T) {
	workspace, root := t.TempDir(), filepath.Join(t.TempDir(), "sessions")
	if err := os.WriteFile(filepath.Join(workspace, "probe.txt"), []byte("probe"), 0600); err != nil {
		t.Fatal(err)
	}
	var captured *builtin.Workspace
	hooks := chatResourceHooks{newExecExecutor: func(value *builtin.Workspace, _ builtin.CommandEnvironment, _ tool.ExecOutputRenderer) (tool.ExecExecutor, error) {
		captured = value
		return nil, errors.New("private-constructor-cause")
	}}
	resources, err := openChatResourcesWithHooks(context.Background(), config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: "fixture-model", BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key")}}, dataPaths{sessionRoot: root}, "", false, workspace, hooks)
	if err == nil || resources != nil || captured == nil || strings.Contains(err.Error(), "private-constructor-cause") {
		t.Fatal("construction failure did not fail closed")
	}
	reader, err := builtin.NewReadExecutor(captured)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := tool.NewReadInput("probe.txt", 1, 1)
	ready, _ := tool.NewReadReadyCall("probe", input)
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	invocation, _ := tool.NewReadInvocation(id, ready)
	if result := reader.Execute(context.Background(), invocation); result.Status() == tool.ResultSuccess {
		t.Fatal("constructor failure leaked open workspace")
	}
	repository, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	thread := domain.ThreadID("00000000-0001-7000-8000-000000000002")
	lease, err := repository.Create(context.Background(), thread)
	if err != nil {
		t.Fatalf("construction failure retained repository ownership: %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}
