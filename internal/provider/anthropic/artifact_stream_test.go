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

func TestReadArtifactSSEChunkingAndRejectedSamples(t *testing.T) {
	for _, mode := range []string{"valid", "duplicate", "partial", "early-terminal"} {
		for _, chunkLimit := range []int{1, 17} {
			t.Run(fmt.Sprintf("%s/%d", mode, chunkLimit), func(t *testing.T) {
				input := `{"artifact_id":"0123456789abcdef0123456789abcdef","offset":0,"limit":3}`
				parts := []string{`{"artifact_id":"0123456789`, `abcdef0123456789abcdef`, `","offset":0,"limit":3}`}
				if strings.Join(parts, "") != input {
					t.Fatal("stream fixture changed command input")
				}
				count := 1
				if mode == "duplicate" {
					count = 2
				}
				events := []string{`{"type":"message_start","message":{"id":"msg-exec","model":"fixture-model"}}`}
				for index := 0; index < count; index++ {
					events = append(events, fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":"call-exec","name":"ReadArtifact","input":{}}}`, index))
					for _, part := range parts {
						events = append(events, fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`, index, quoteExecJSON(t, part)))
					}
					if mode != "partial" {
						events = append(events, fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index))
					}
				}
				if mode != "partial" {
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
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
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
				}))
				defer server.Close()
				instance, err := New(Config{BaseURL: server.URL + "/prefix", APIKey: secret.New("fixture-key"), Model: "fixture-model"})
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				conversation := instance.NewConversation().(*Conversation)
				turn, err := (provider.TurnInput{Text: "test"}).WithToolCatalog(testAnthropicToolCatalog(t))
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
							t.Fatal("invalid ReadArtifact sample committed")
						}
						continue
					}
					if event.Kind() != provider.StreamEventCompleted || event.PreparedSample() == nil {
						t.Fatalf("valid ReadArtifact stream failed: %v", event.Error())
					}
					ready, err := event.PreparedSample().ReadyCalls()
					if err != nil || len(ready) != 1 || ready[0].Capability() != tool.CapabilityReadArtifact || ready[0].ReadArtifactInput().Limit() != 3 {
						t.Fatal("chunked ReadArtifact changed")
					}
					if err := event.PreparedSample().Finalize(); err != nil {
						t.Fatal(err)
					}
				}
				if terminals != 1 {
					t.Fatalf("terminal count=%d", terminals)
				}
				want := 0
				if mode == "valid" {
					want = 1
				}
				if len(conversation.historySnapshot()) != want {
					t.Fatal("invalid ReadArtifact stream changed native history")
				}
			})
		}
	}
}
