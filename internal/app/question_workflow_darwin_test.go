//go:build darwin

package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/tool"
)

func TestQuestionReadWritePatchExecWorkflowKeepsApprovalIndependent(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			workspace := privateAppTempDir(t)
			root := filepath.Join(privateAppTempDir(t), "sessions")
			if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("old\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var requests [][]byte
			name := "request_user_input"
			model := "gpt-test"
			if family == domain.ProviderAnthropic {
				name = "AskUserQuestion"
				model = "claude-test"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				mu.Lock()
				requests = append(requests, body)
				sample := len(requests)
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				switch sample {
				case 1:
					serveWriteScenarioSample(w, family, sample, []writeScenarioCall{{"Read", `{"file_path":"main.go"}`}, {name, questionScenarioInput}})
				case 2:
					serveWriteScenarioSample(w, family, sample, []writeScenarioCall{{"Write", `{"file_path":"answer.txt","content":"chosen"}`}, {name, questionScenarioInput}})
				case 3:
					if family == domain.ProviderOpenAI {
						writeOpenAIPatchSample(w, 1)
					} else {
						writeAnthropicPatchSample(w, 1)
					}
				case 4:
					writeExecCommandSample(w, family, `test "$(cat main.go)" = new && test "$(cat answer.txt)" = chosen && printf done > question-exec-marker`, 1000)
				default:
					serveWriteScenarioSample(w, family, sample, nil)
				}
			}))
			defer server.Close()
			cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, APIKey: secret.New("workflow-fixture"), Model: model}}, true)
			resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{interactiveApprovals: true})
			if err != nil {
				t.Fatal(err)
			}
			defer resources.close(context.Background())
			host, err := startTUIControlSession(context.Background(), resources.runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer host.Shutdown(context.Background())
			events, err := submitInteractiveFixture(t, host, context.Background(), "Read, ask, write, ask, edit, execute and summarize")
			if err != nil {
				t.Fatal(err)
			}
			asked, resolved := 0, 0
			var approvals []string
			var previous protocol.UserQuestionRequestedPayload
			for e := range events {
				switch e.Kind {
				case protocol.EventUserQuestionRequested:
					asked++
					journalPath, _ := resources.service.repository.JournalPath(resources.identity.ThreadID)
					before, err := os.ReadFile(journalPath)
					if err != nil {
						t.Fatal(err)
					}
					probe := exec.Command(os.Args[0], "-test.run=^TestHostLeaseProbe$")
					probe.Env = append(os.Environ(), "EASYCODE_HOST_LEASE_ROOT="+root, "EASYCODE_HOST_LEASE_THREAD="+string(resources.identity.ThreadID), "EASYCODE_HOST_LEASE_EXPECT=busy")
					if output, err := probe.CombinedOutput(); err != nil {
						t.Fatal("waiting question lost cross-process lease", err, string(output))
					}
					after, err := os.ReadFile(journalPath)
					if err != nil || !bytes.Equal(before, after) {
						t.Fatal("contender changed question journal", err)
					}
					p, _ := protocol.DecodeUserQuestionRequested(e)
					a, _ := tool.NewQuestionAnswerSet([]tool.QuestionAnswer{{QuestionID: "storage", SelectedOptions: []int{0}}})
					if asked == 2 {
						if err := host.ResolveQuestion(context.Background(), e.TurnID, previous, a); err == nil {
							t.Fatal("old response resolved second question")
						}
						if _, err := os.Stat(filepath.Join(workspace, "answer.txt")); err != nil {
							t.Fatal("second question overtook prior Write")
						}
					}
					if err := host.ResolveQuestion(context.Background(), e.TurnID, p, a); err != nil {
						t.Fatal(err)
					}
					previous = p
				case protocol.EventUserQuestionResolved:
					resolved++
				case protocol.EventToolApprovalRequested:
					p, _ := protocol.DecodeToolApprovalRequested(e)
					approvals = append(approvals, p.Capability)
					if err := host.ResolveApproval(context.Background(), e.TurnID, p, protocol.ApprovalAllowOnce, ""); err != nil {
						t.Fatal(err)
					}
				case protocol.EventTurnFailed:
					t.Fatal("workflow failed", string(e.Payload))
				}
			}
			if err := host.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := resources.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if asked != 2 || resolved != 2 || strings.Join(approvals, ",") != "fs.write,fs.patch,process.exec" {
				t.Fatal("answer changed permission flow", asked, resolved, approvals)
			}
			marker, err := os.ReadFile(filepath.Join(workspace, "question-exec-marker"))
			if err != nil || string(marker) != "done" {
				t.Fatal("dependent command failed", err)
			}
			_, plan := loadToolLoopPlan(t, root, resources.identity.ThreadID)
			want := []tool.CapabilityID{tool.CapabilityRead, tool.CapabilityQuestion, tool.CapabilityWrite, tool.CapabilityQuestion, tool.CapabilityPatch, tool.CapabilityExec}
			if len(plan.Tools) != len(want) || plan.ToolRecovery != nil {
				t.Fatal("incomplete workflow ledger")
			}
			for i, entry := range plan.Tools {
				if entry.Invocation.Capability() != want[i] || !entry.HasResult || entry.Result.Status() != tool.ResultSuccess {
					t.Fatal("reordered or failed workflow slot", i)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) != 5 {
				t.Fatal("unexpected repeated samples", len(requests))
			}
		})
	}
}
