package app

import (
	"context"
	"fmt"
	"io"
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
)

type writeScenarioCall struct{ name, arguments string }

func serveWriteScenarioSample(writer io.Writer, family domain.ProviderFamily, sample int, calls []writeScenarioCall) {
	if calls == nil {
		if family == domain.ProviderOpenAI {
			writeOpenAIAppTurn(writer, sample)
		} else {
			writeAnthropicAppTurn(writer, sample)
		}
		return
	}
	if family == domain.ProviderOpenAI {
		fmt.Fprintf(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"write-%d\"}}\n\n", sample)
	} else {
		fmt.Fprintf(writer, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"write-%d\",\"model\":\"claude-test\"}}\n\n", sample)
	}
	for i, call := range calls {
		id := fmt.Sprintf("write-%d-%d", sample, i)
		if family == domain.ProviderOpenAI {
			writeOpenAIFunctionCall(writer, i, "item-"+id, id, call.name, call.arguments)
		} else {
			writeAnthropicToolUse(writer, i, id, call.name, call.arguments)
		}
	}
	if family == domain.ProviderOpenAI {
		fmt.Fprintf(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"write-%d\"}}\n\n", sample)
	} else {
		io.WriteString(writer, "data: {\"type\":\"message_stop\"}\n\n")
	}
}

func TestWriteEndToEndCreateReadOverwriteAndVerify(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			root := t.TempDir()
			dataRoot := t.TempDir()
			var recorder requestRecorder
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requestIndex, _ := recorder.record(t, request)
				sample := requestIndex + 1
				writer.Header().Set("Content-Type", "text/event-stream")
				var calls []writeScenarioCall
				switch sample {
				case 1:
					calls = []writeScenarioCall{{"Write", `{"file_path":"new/pkg/a","content":"初始\r\n"}`}, {"Write", `{"file_path":"empty","content":""}`}}
				case 2:
					calls = []writeScenarioCall{{"Read", `{"file_path":"new/pkg/a"}`}, {"Read", `{"file_path":"empty"}`}}
				case 3:
					calls = []writeScenarioCall{{"Write", `{"file_path":"new/pkg/a","content":"完成\n"}`}, {"Write", `{"file_path":"empty","content":"filled"}`}}
				case 4:
					calls = []writeScenarioCall{{"Read", `{"file_path":"new/pkg/a"}`}, {"Read", `{"file_path":"empty"}`}}
				}
				serveWriteScenarioSample(writer, family, sample, calls)
			}))
			defer server.Close()
			model := "gpt-test"
			if family == domain.ProviderAnthropic {
				model = "claude-test"
			}
			resources := mustOpenChatResourcesWithHooks(t, context.Background(), cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, APIKey: secret.New("write-e2e-fixture"), Model: model}}, false), dataPaths{sessionRoot: filepath.Join(dataRoot, "sessions")}, "", false, root, chatResourceHooks{interactiveApprovals: true})
			control, err := startTUIControlSession(context.Background(), resources.runtime)
			if err != nil {
				t.Fatal(err)
			}
			events, err := submitInteractiveFixture(t, control, context.Background(), "Create and then rewrite the files")
			if err != nil {
				t.Fatal(err)
			}
			approvals := 0
			completed := false
			for event := range events {
				switch event.Kind {
				case protocol.EventToolApprovalRequested:
					p, err := protocol.DecodeToolApprovalRequested(event)
					if err != nil {
						t.Fatal(err)
					}
					approvals++
					if p.Capability != "fs.write" || p.Presentation.Kind != protocol.PresentationWriteCreate {
						t.Fatal("首次创建展示错误", p.Capability, p.Presentation.Kind)
					}
					if err := control.ResolveApproval(context.Background(), event.TurnID, p, protocol.ApprovalAllowSession, ""); err != nil {
						t.Fatal(err)
					}
				case protocol.EventTurnCompleted:
					completed = true
				case protocol.EventTurnFailed:
					p, _ := protocol.DecodeTurnFailed(event)
					t.Errorf("Write 回合失败 %s", p.Message)
				}
			}
			if err := control.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			closeAppTestResources(t, resources)
			captured := recorder.snapshot()
			if approvals != 1 || !completed || len(captured) != 5 {
				t.Fatalf("approvals=%d completed=%v requests=%d", approvals, completed, len(captured))
			}
			for path, want := range map[string]string{"new/pkg/a": "完成\n", "empty": "filled"} {
				got, err := os.ReadFile(filepath.Join(root, path))
				if err != nil || string(got) != want {
					t.Fatalf("目标 %s: %q %v", path, got, err)
				}
			}
			if !strings.Contains(string(captured[4]), "Overwrote new/pkg/a") || !strings.Contains(string(captured[4]), "filled") {
				t.Fatal("后续请求缺少冻结结果与最终 Read")
			}
			loaded, _ := loadToolLoopPlan(t, filepath.Join(dataRoot, "sessions"), resources.identity.ThreadID)
			count := 0
			for _, record := range loaded.Records {
				if record.EventKind == session.EventToolCallResult {
					count++
				}
			}
			if count != 8 {
				t.Fatal("调用 ledger 数量错误", count)
			}
		})
	}
}
