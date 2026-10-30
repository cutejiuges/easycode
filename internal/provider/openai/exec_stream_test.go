package openai

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
	for _, mode := range []string{"valid", "duplicate", "partial", "early-terminal"} {
		for _, chunkLimit := range []int{1, 17} {
			t.Run(fmt.Sprintf("%s/%d", mode, chunkLimit), func(t *testing.T) {
				input := `{"command":"printf '你好'","workdir":"src","timeout_ms":1000}`
				parts := []string{`{"command":"printf '`, `你好`, `'","workdir":"src","timeout_ms":1000}`}
				count := 1
				if mode == "duplicate" {
					count = 2
				}

				events := []string{`{"type":"response.created","response":{"id":"resp-exec"}}`}
				for index := 0; index < count; index++ {
					id := fmt.Sprintf("fc-%d", index)
					events = append(events, fmt.Sprintf(`{"type":"response.output_item.added","output_index":%d,"item":{"type":"function_call","id":%q,"call_id":"call-exec","name":"exec_command"}}`, index, id))
					for _, part := range parts {
						events = append(events, fmt.Sprintf(`{"type":"response.function_call_arguments.delta","item_id":%q,"output_index":%d,"delta":%s}`, id, index, quoteExecJSON(t, part)))
					}
					if mode != "partial" {
						events = append(events, fmt.Sprintf(`{"type":"response.function_call_arguments.done","item_id":%q,"output_index":%d,"arguments":%s}`, id, index, quoteExecJSON(t, input)), fmt.Sprintf(`{"type":"response.output_item.done","output_index":%d,"item":{"type":"function_call","id":%q,"call_id":"call-exec","name":"exec_command","arguments":%s}}`, index, id, quoteExecJSON(t, input)))
					}
				}
				if mode != "partial" {
					events = append(events, `{"type":"response.completed","response":{"id":"resp-exec"}}`)
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
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					if request.URL.Path != "/prefix/responses" {
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
				}))
				defer server.Close()
				instance, err := New(Config{BaseURL: server.URL + "/prefix", APIKey: secret.New("fixture-key"), Model: "fixture-model"})
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				conversation := instance.NewConversation().(*Conversation)
				testCommitUserInput(t, conversation, "test")
				turn, err := provider.NewSamplingInput().WithToolCatalog(testOpenAIToolCatalog(t))
				if err != nil {
					t.Fatal(err)
				}
				stream, err := conversation.Stream(context.Background(), turn)
				if err != nil {
					t.Fatal(err)
				}
				terminals := 0
				for event := range stream {
					if !event.Kind().Terminal() {
						continue
					}
					terminals++
					if mode != "valid" {
						if event.Kind() == provider.StreamEventCompleted || event.PreparedSample() != nil {
							t.Fatal("invalid Exec sample committed")
						}
						continue
					}
					if event.Kind() != provider.StreamEventCompleted || event.PreparedSample() == nil {
						t.Fatalf("valid Exec stream failed: %v", event.Error())
					}
					ready, err := event.PreparedSample().ReadyCalls()
					if err != nil || len(ready) != 1 || ready[0].Capability() != tool.CapabilityExec || ready[0].ExecInput().Command() != "printf '你好'" {
						t.Fatal("chunked UTF-8 Exec changed")
					}
					if err := event.PreparedSample().Finalize(); err != nil {
						t.Fatal(err)
					}
				}
				if terminals != 1 {
					t.Fatalf("terminal count=%d", terminals)
				}
				want := 1
				if mode == "valid" {
					want = 2
				}
				if len(conversation.historySnapshot()) != want {
					t.Fatal("invalid Exec stream changed native history")
				}
			})
		}
	}
}
