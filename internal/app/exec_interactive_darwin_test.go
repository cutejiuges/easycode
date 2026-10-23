//go:build darwin

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestMain(m *testing.M) {
	if handled, err := builtin.RunCommandChild(os.Args[1:]); handled {
		if err != nil {
			os.Exit(125)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestReadPatchExecApprovedLoopForBothProviders(t *testing.T) {
	const command = `test "$(cat main.go)" = new && env > command-environment && printf verified > exec-marker && printf checks-passed`
	for _, name := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "AUTHORIZATION", "COOKIE", "HTTPS_PROXY", "ALL_PROXY", "EASYCODE_INTERNAL_SESSION", "BASH_ENV", "DYLD_INSERT_LIBRARIES"} {
		t.Setenv(name, "command-private-secret")
	}
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			workspace := t.TempDir()
			for name, content := range map[string]string{"README.md": "run local checks\n", "main.go": "old\n"} {
				if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var mu sync.Mutex
			var requests [][]byte
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				requests = append(requests, body)
				sample := len(requests)
				mu.Unlock()
				writer.Header().Set("Content-Type", "text/event-stream")
				if family == domain.ProviderOpenAI {
					switch sample {
					case 1:
						writeOpenAIToolLoopSample(writer, 1)
					case 2:
						writeOpenAIPatchSample(writer, 1)
					case 3:
						writeExecCommandSample(writer, family, command, 1000)
					default:
						writeOpenAIAppTurn(writer, sample)
					}
				} else {
					switch sample {
					case 1:
						writeAnthropicToolLoopSample(writer, 1)
					case 2:
						writeAnthropicPatchSample(writer, 1)
					case 3:
						writeExecCommandSample(writer, family, command, 1000)
					default:
						writeAnthropicAppTurn(writer, sample)
					}
				}
			}))
			defer server.Close()
			root := filepath.Join(t.TempDir(), "sessions")
			model := "gpt-test"
			if family == domain.ProviderAnthropic {
				model = "claude-test"
			}
			resources, err := openChatResourcesWithHooks(context.Background(), config.Config{Provider: config.Provider{
				Family: family, Model: model, BaseURL: server.URL, APIKey: secret.New("exec-private-key"),
			}}, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{interactiveApprovals: true})
			if err != nil {
				t.Fatal(err)
			}
			defer resources.close(context.Background())
			control, err := startTUIControlSession(context.Background(), resources.runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer control.stop(context.Background())
			events, err := control.Submit(context.Background(), "Read, edit, run checks, and summarize")
			if err != nil {
				t.Fatal(err)
			}
			var approvals []string
			var commandStarted, commandTerminal, completed int
			var completedUsage domain.SampleUsage
			var ui tea.Model = tui.NewModel(context.Background(), "test", control)
			ui, _ = ui.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Read, edit, run checks, and summarize")})
			ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
			for event := range events {
				ui, _ = ui.Update(tui.RuntimeEventMsg{Event: event})
				switch event.Kind {
				case protocol.EventToolApprovalRequested:
					payload, err := protocol.DecodeToolApprovalRequested(event)
					if err != nil {
						t.Fatal(err)
					}
					approvals = append(approvals, payload.Capability)
					if err := control.ResolveApproval(context.Background(), event.TurnID, payload, protocol.ApprovalAllowOnce, ""); err != nil {
						t.Fatal(err)
					}
				case protocol.EventCommandStarted:
					commandStarted++
				case protocol.EventCommandTerminal:
					commandTerminal++
					payload, err := protocol.DecodeCommandActivity(event)
					if err != nil || payload.Status != "success" || !payload.ExitKnown || payload.ExitCode != 0 {
						t.Fatalf("command terminal = %#v %v", payload, err)
					}
				case protocol.EventTurnCompleted:
					completed++
					payload, err := protocol.DecodeTurnCompleted(event)
					if err != nil {
						t.Fatal(err)
					}
					completedUsage, err = payload.Usage.Domain()
					if err != nil {
						t.Fatal(err)
					}
				case protocol.EventTurnFailed:
					t.Fatalf("turn failed: %s", event.Payload)
				}
			}
			if err := control.stop(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := resources.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if strings.Join(approvals, ",") != "fs.patch,process.exec" || commandStarted != 1 || commandTerminal != 1 || completed != 1 {
				t.Fatalf("approval/lifecycle = %v %d/%d/%d", approvals, commandStarted, commandTerminal, completed)
			}
			if !strings.Contains(ui.View(), "Status: idle") || !strings.Contains(ui.View(), "answer-4") || !strings.Contains(ui.View(), "exit=0") {
				t.Fatalf("final TUI lost completed command: %s", ui.View())
			}
			ui, _ = ui.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
			if !strings.Contains(ui.View(), "checks-passed") || !strings.Contains(ui.View(), "command-environment") {
				t.Fatal("TUI details lost full command or frozen result")
			}
			for name, want := range map[string]string{"main.go": "new\n", "exec-marker": "verified"} {
				got, err := os.ReadFile(filepath.Join(workspace, name))
				if err != nil || string(got) != want {
					t.Fatalf("%s = %q %v", name, got, err)
				}
			}
			mu.Lock()
			captured := append([][]byte(nil), requests...)
			mu.Unlock()
			if len(captured) != 4 {
				t.Fatalf("requests = %d", len(captured))
			}
			var toolsBaseline []byte
			for _, body := range captured {
				var request struct {
					Tools json.RawMessage `json:"tools"`
				}
				if err := json.Unmarshal(body, &request); err != nil {
					t.Fatal(err)
				}
				if toolsBaseline == nil {
					toolsBaseline = bytes.Clone(request.Tools)
				}
				if !bytes.Equal(toolsBaseline, request.Tools) || bytes.Contains(request.Tools, []byte(command)) || bytes.Contains(request.Tools, []byte(workspace)) {
					t.Fatal("command state invalidated stable tool catalog")
				}
				var facades []json.RawMessage
				if err := json.Unmarshal(request.Tools, &facades); err != nil || len(facades) != 5 {
					t.Fatal("request catalog lost a real capability")
				}
			}
			last := string(captured[3])
			for _, required := range []string{"call-exec", "Exec outcome: success (ok)", "new"} {
				if !strings.Contains(last, required) {
					t.Fatalf("continuation lost %q", required)
				}
			}
			if strings.Contains(last, workspace) || strings.Contains(last, "exec-private-key") {
				t.Fatal("continuation leaked host data")
			}
			loaded, plan := loadToolLoopPlan(t, root, resources.identity.ThreadID)
			var ready, started, results, decisions int
			for _, record := range loaded.Records {
				switch record.EventKind {
				case session.EventToolCallReady:
					ready++
				case session.EventToolExecutionStarted:
					started++
				case session.EventToolCallResult:
					results++
				case session.EventToolApprovalDecided:
					decisions++
				}
			}
			if ready != 3 || started != 3 || results != 3 || decisions != 2 || plan.ToolRecovery != nil || len(plan.NativeCommits) != 7 || len(plan.SampleUsages) != 4 {
				t.Fatalf("ledger counts = %d/%d/%d/%d, commits=%d usage=%d", ready, started, results, decisions, len(plan.NativeCommits), len(plan.SampleUsages))
			}
			var usages []domain.SampleUsage
			for _, usage := range plan.SampleUsages {
				usages = append(usages, usage.Usage)
			}
			aggregate, err := domain.AggregateSampleUsage(usages)
			if err != nil || aggregate != completedUsage {
				t.Fatal("turn usage diverged from durable samples")
			}
			assertExecLoopNativeOrder(t, plan, family)
			repository, err := session.OpenOrCreateRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			journalPath, err := repository.JournalPath(resources.identity.ThreadID)
			if err != nil {
				t.Fatal(err)
			}
			if err := repository.Close(); err != nil {
				t.Fatal(err)
			}
			baseline, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			environment, err := os.ReadFile(filepath.Join(workspace, "command-environment"))
			if err != nil {
				t.Fatal(err)
			}
			for _, content := range [][]byte{environment, baseline, []byte(ui.View()), bytes.Join(captured, nil)} {
				if bytes.Contains(content, []byte("command-private-secret")) || bytes.Contains(content, []byte("exec-private-key")) {
					t.Fatal("secret escaped environment boundary")
				}
			}
			var execID tool.InvocationID
			var frozen string
			var checkpoints []session.Record
			for _, record := range loaded.Records {
				if record.EventKind == session.EventToolCallReady {
					payload, err := session.DecodeToolCallReadyPayload(record)
					if err != nil {
						t.Fatal(err)
					}
					if payload.Capability == tool.CapabilityExec {
						execID = payload.InvocationID
						checkpoints = append(checkpoints, record)
					}
				} else if execID != "" {
					switch record.EventKind {
					case session.EventToolApprovalDecided, session.EventToolExecutionStarted:
						checkpoints = append(checkpoints, record)
					case session.EventToolCallResult:
						payload, err := session.DecodeToolCallResultPayload(record)
						if err != nil {
							t.Fatal(err)
						}
						frozen = payload.Preview
						checkpoints = append(checkpoints, record)
					}
				}
			}
			if len(checkpoints) != 4 {
				t.Fatalf("Exec checkpoints = %d", len(checkpoints))
			}
			for _, checkpoint := range checkpoints {
				t.Run("recover-"+string(checkpoint.EventKind), func(t *testing.T) {
					writeJournalPrefix(t, journalPath, baseline, checkpoint.Sequence)
					if err := os.WriteFile(filepath.Join(workspace, "exec-marker"), []byte("must-not-rerun"), 0600); err != nil {
						t.Fatal(err)
					}
					resumed, err := openChatResources(context.Background(), config.Config{Provider: config.Provider{
						Family: family, Model: model, BaseURL: server.URL, APIKey: secret.New("exec-private-key"),
					}}, root, string(resources.identity.ThreadID), workspace)
					if err != nil {
						t.Fatal(err)
					}
					if err := resumed.close(context.Background()); err != nil {
						t.Fatal(err)
					}
					mu.Lock()
					count := len(requests)
					mu.Unlock()
					marker, err := os.ReadFile(filepath.Join(workspace, "exec-marker"))
					if err != nil || string(marker) != "must-not-rerun" || count != 4 {
						t.Fatal("recovery repeated process or Provider side effects")
					}
					recovered, recoveryPlan := loadToolLoopPlan(t, root, resources.identity.ThreadID)
					if recoveryPlan.ToolRecovery != nil || recoveryPlan.InterruptedTail != nil {
						t.Fatal("recovery did not seal interrupted turn")
					}
					var found bool
					for _, record := range recovered.Records {
						if record.EventKind != session.EventToolCallResult {
							continue
						}
						payload, err := session.DecodeToolCallResultPayload(record)
						if err != nil {
							t.Fatal(err)
						}
						if payload.InvocationID != execID {
							continue
						}
						found = true
						if checkpoint.EventKind == session.EventToolExecutionStarted && payload.Status != tool.ResultOutcomeUncertain {
							t.Fatal("started command was not marked uncertain")
						}
						if checkpoint.EventKind == session.EventToolCallResult && payload.Preview != frozen {
							t.Fatal("durable command preview changed on recovery")
						}
					}
					if !found {
						t.Fatal("missing compensated command result")
					}
					sealed, err := os.ReadFile(journalPath)
					if err != nil {
						t.Fatal(err)
					}
					lines := bytes.SplitAfter(baseline, []byte("\n"))
					if !bytes.HasPrefix(sealed, bytes.Join(lines[:checkpoint.Sequence], nil)) {
						t.Fatal("recovery rewrote committed journal bytes")
					}
				})
			}
		})
	}
}
