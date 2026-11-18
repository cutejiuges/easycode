package anthropic

import (
	"context"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/tool"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExecSSEChunkingAndRejectedSamples(t *testing.T) {
	testToolSSESampleModes(t, "Bash", `{"command":"printf '你好'","workdir":"src","timeout_ms":1000}`, []string{`{"command":"printf '`, `你好`, `'","workdir":"src","timeout_ms":1000}`}, tool.CapabilityExec, func(call tool.ReadyCall) bool { return call.ExecInput().Command() == "printf '你好'" })
}

func TestTaskSSEChunkingAndRejectedSamples(t *testing.T) {
	testToolSSESampleModes(t, "TodoWrite", taskNativeInput, []string{taskNativeInput[:12], taskNativeInput[12:]}, tool.CapabilityTaskProgress, func(call tool.ReadyCall) bool {
		return call.TaskInput().Update().Items()[0].Content() == "检查中文代码"
	})
}

func testToolSSESampleModes(t *testing.T, name, input string, parts []string, capability tool.CapabilityID, check func(tool.ReadyCall) bool) {
	t.Helper()

	for _, mode := range []string{"valid", "duplicate", "partial", "early-terminal", "cancelled"} {
		for _, chunkLimit := range []int{1, 17} {
			t.Run(fmt.Sprintf("%s/%d", mode, chunkLimit), func(t *testing.T) {
				if strings.Join(parts, "") != input {
					t.Fatal("stream fixture changed command input")
				}
				count := 1
				if mode == "duplicate" {
					count = 2
				}
				events := []string{`{"type":"message_start","message":{"id":"msg-exec","model":"fixture-model"}}`}
				for index := 0; index < count; index++ {
					events = append(events, fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":"call-exec","name":%q,"input":{}}}`, index, name))
					for _, part := range parts {
						events = append(events, fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`, index, quoteExecJSON(t, part)))
					}
					if mode != "partial" && mode != "cancelled" {
						events = append(events, fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index))
					}
				}
				if mode != "partial" && mode != "cancelled" {
					events = append(events, `{"type":"message_stop"}`)
				}

				if mode == "early-terminal" {
					events = append(events[:2], events[len(events)-1])
				}
				var body strings.Builder
				for _, event := range events {
					body.WriteString("data: ")
					body.WriteString(event)
					body.WriteString("\n\n")
				}
				started, cleaned := make(chan struct{}), make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					defer close(cleaned)
					if request.URL.Path != "/prefix/messages" {
						t.Error("API prefix lost")
					}
					writer.Header().Set("Content-Type", "text/event-stream")
					data := []byte(body.String())
					random := rand.New(rand.NewSource(19))
					for len(data) > 0 {
						size := min(len(data), 1+random.Intn(chunkLimit))
						if _, err := writer.Write(data[:size]); err != nil {
							return
						}
						writer.(http.Flusher).Flush()
						data = data[size:]
					}
					if mode == "cancelled" {
						close(started)
						<-request.Context().Done()
					}
				}))
				defer server.Close()
				instance, err := New(Config{BaseURL: server.URL + "/prefix", APIKey: secret.New("fixture-key"), Model: "fixture-model"})
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				conversation := testConversation(t, instance)
				testCommitUserInput(t, conversation, "test")
				turn, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testAnthropicToolCatalog(t))
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				stream, err := conversation.Stream(ctx, turn)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "cancelled" {
					<-started
					cancel()
				}
				terminals := 0
				for event := range stream {
					if !event.Kind().Terminal() {
						continue
					}
					terminals++
					if mode != "valid" {
						if event.Kind() == provider.StreamEventCompleted || event.PreparedSample() != nil {
							t.Fatal("invalid tool sample committed")
						}
						continue
					}
					if event.Kind() != provider.StreamEventCompleted || event.PreparedSample() == nil {
						t.Fatalf("valid tool stream failed: %v", event.Error())
					}
					ready, err := event.PreparedSample().ReadyCalls()
					if err != nil || len(ready) != 1 || ready[0].Capability() != capability || !check(ready[0]) {
						t.Fatal("chunked UTF-8 tool changed")
					}
					if err := event.PreparedSample().Finalize(); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "cancelled" {
					<-cleaned
				}
				if terminals != 1 {
					t.Fatalf("terminal count=%d", terminals)
				}
				want := 1
				if mode == "valid" {
					want = 2
				}
				if len(conversation.historySnapshot()) != want {
					t.Fatal("invalid tool stream changed native history")
				}
			})
		}
	}
}
