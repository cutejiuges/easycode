package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool"
)

const questionScenarioInput = `{"questions":[{"id":"storage","header":"存储","question":"选择存储方式？","options":[{"label":"SQLite (Recommended)","description":"本地文件"},{"label":"PostgreSQL","description":"独立服务"}],"multi_select":false}]}`

func TestQuestionEndToEndBothProviders(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, mode := range []string{"selection", "other", "skip", "cancel", "unavailable"} {
			t.Run(string(family)+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				workspace := t.TempDir()
				root := filepath.Join(t.TempDir(), "sessions")
				writeAppTestFile(t, filepath.Join(workspace, "README.md"), []byte("source material"), 0600)
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
					assertReasoningRequest(t, family, body, false)
					mu.Lock()
					requests = append(requests, body)
					sample := len(requests)
					mu.Unlock()
					w.Header().Set("Content-Type", "text/event-stream")
					var calls []writeScenarioCall
					switch sample {
					case 1:
						calls = []writeScenarioCall{{"Read", `{"file_path":"README.md"}`}, {name, questionScenarioInput}}
					case 2:
						calls = []writeScenarioCall{{"Write", `{"file_path":"answer.txt","content":"chosen"}`}}
					}
					var encoded bytes.Buffer
					serveWriteScenarioSample(&encoded, family, sample, calls)
					// 固定变化的字节切片故意切断 UTF-8 和 SSE 边界。
					data := withReasoningFixture(family, encoded.Bytes())
					for offset := 0; offset < len(data); {
						n := min(1+offset%23, len(data)-offset)
						if _, err := w.Write(data[offset : offset+n]); err != nil {
							return
						}
						offset += n
					}
				}))
				defer server.Close()
				cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, APIKey: secret.New("question-fixture"), Model: model}}, true)
				cfg = reasoningFixtureConfig(t, cfg)
				resources := mustOpenChatResourcesWithHooks(t, ctx, cfg, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{interactiveApprovals: mode != "unavailable"})
				defer func() { _ = resources.close(context.Background()) }()
				control, err := startTUIControlSession(ctx, resources.runtime)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = control.Shutdown(context.Background()) }()
				events, err := submitInteractiveFixture(t, control, ctx, "Read, ask and write")
				if err != nil {
					t.Fatal(err)
				}
				asked, resolved, approvals, completed := 0, 0, 0, false
				for e := range events {
					switch e.Kind {
					case protocol.EventUserQuestionRequested:
						asked++
						p, err := protocol.DecodeUserQuestionRequested(e)
						if err != nil {
							t.Fatal(err)
						}
						if mode == "cancel" {
							control.Interrupt()
							continue
						}
						answer := tool.QuestionAnswer{QuestionID: "storage"}
						switch mode {
						case "selection":
							answer.SelectedOptions = []int{0}
							answer.Text = " 保留说明\n"
						case "other":
							answer.Text = "  自定义 🧑‍💻\n"
						case "skip":
							answer.Skipped = true
						default:
							t.Fatal("headless requested input")
						}
						a, err := tool.NewQuestionAnswerSet([]tool.QuestionAnswer{answer})
						if err != nil {
							t.Fatal(err)
						}
						if err := control.ResolveQuestion(ctx, e.TurnID, p, a); err != nil {
							t.Fatal(err)
						}
					case protocol.EventUserQuestionResolved:
						resolved++
					case protocol.EventToolApprovalRequested:
						approvals++
						p, _ := protocol.DecodeToolApprovalRequested(e)
						if err := control.ResolveApproval(ctx, e.TurnID, p, protocol.ApprovalAllowOnce, ""); err != nil {
							t.Fatal(err)
						}
					case protocol.EventTurnCompleted:
						completed = true
					case protocol.EventTurnFailed:
						if mode != "cancel" {
							t.Fatal("question turn failed", string(e.Payload))
						}
					}
				}
				if err := control.Shutdown(ctx); err != nil {
					t.Fatal(err)
				}
				thread := resources.identity.ThreadID
				if err := resources.close(ctx); err != nil {
					t.Fatal(err)
				}
				loaded, plan := loadToolLoopPlan(t, root, thread)
				if len(plan.Tools) < 2 || plan.Tools[0].Invocation.Capability() != tool.CapabilityRead || plan.Tools[1].Invocation.Capability() != tool.CapabilityQuestion {
					t.Fatal("ordered tool transcript changed")
				}
				q := plan.Tools[1]
				if !q.HasResult {
					t.Fatal("missing frozen answer")
				}
				if mode == "unavailable" {
					if asked != 0 || resolved != 0 || q.Result.Code() != "user_input_unavailable" || approvals != 0 {
						t.Fatal("headless waited for input")
					}
				} else if asked != 1 || resolved != 1 {
					t.Fatalf("question events %d/%d", asked, resolved)
				}
				if mode == "cancel" {
					if q.Result.Status() != tool.ResultCancelled || approvals != 0 {
						t.Fatal("cancel invented answer or granted permission")
					}
				} else if !completed {
					t.Fatal("turn did not finish")
				}
				if mode != "unavailable" && mode != "cancel" {
					if approvals != 1 {
						t.Fatal("answer bypassed Write approval")
					}
					content, err := os.ReadFile(filepath.Join(workspace, "answer.txt"))
					if err != nil || string(content) != "chosen" {
						t.Fatal("dependent Write failed", err)
					}
					a, err := q.Result.QuestionAnswers()
					if err != nil {
						t.Fatal(err)
					}
					if mode == "other" && a.Answers()[0].Text != "  自定义 🧑‍💻\n" {
						t.Fatal("answer text changed")
					}
				}
				for _, record := range loaded.Records {
					if record.EventKind == session.EventToolApprovalDecided {
						var p session.ToolApprovalDecidedPayload
						if err := codec.Unmarshal(record.Payload, &p); err != nil {
							t.Fatal(err)
						}
						if string(p.InvocationID) == string(q.Invocation.InvocationID()) {
							t.Fatal("question persisted approval")
						}
					}
				}
				mu.Lock()
				before := len(requests)
				continuation := string(requests[min(1, len(requests)-1)])
				mu.Unlock()
				if mode != "cancel" && !strings.Contains(continuation, string(q.Result.ProviderCallID())) {
					t.Fatal("missing native output pairing")
				}
				if mode == "selection" {
					read := plan.Tools[0]
					callID := strconv.Quote(string(read.Result.ProviderCallID()))
					if !read.HasResult || read.Result.Status() != tool.ResultSuccess ||
						!strings.Contains(read.Result.Preview().Text(), "1\tsource material") ||
						strings.Count(continuation, string(callID)) != 2 || !strings.Contains(continuation, "source material") {
						t.Fatal("Read content or native call/output pairing was lost before Question and Write")
					}
				}
				resumed := mustOpenChatResources(t, ctx, cfg, root, string(thread), workspace)
				if err := resumed.close(ctx); err != nil {
					t.Fatal(err)
				}
				mu.Lock()
				after := len(requests)
				mu.Unlock()
				if before != after {
					t.Fatal("resume called Provider")
				}
				_, restored := loadToolLoopPlan(t, root, thread)
				if restored.Tools[1].Result.Preview() != q.Result.Preview() {
					t.Fatal("restore changed answer preview")
				}
			})
		}
	}
}
