package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/secret"
)

func TestConversationUsesNativeHistoryAcrossTwoTurnsAndBaseURLForms(t *testing.T) {
	for _, test := range []struct {
		name     string
		prefix   string
		wantPath string
	}{
		{name: "host only", wantPath: "/responses"},
		{name: "path prefix", prefix: "/openai/v1/", wantPath: "/openai/v1/responses"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var mu sync.Mutex
			var requests []responsesRequest
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != test.wantPath {
					t.Errorf("request path: got %q want %q", request.URL.Path, test.wantPath)
				}
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Errorf("read request: %v", err)
					return
				}
				var decoded responsesRequest
				if err := codec.Unmarshal(body, &decoded); err != nil {
					t.Errorf("decode request: %v", err)
					return
				}
				mu.Lock()
				requests = append(requests, decoded)
				turn := len(requests)
				mu.Unlock()

				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n\n")
				_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"id\":\"msg-"+string(rune('0'+turn))+"\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"answer\"}]}}\n\n")
				_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp"+string(rune('0'+turn))+"\"}}\n\n")
			}))
			defer server.Close()

			instance, err := New(Config{
				BaseURL: server.URL + test.prefix,
				APIKey:  secret.New("test-key"),
				Model:   "gpt-test",
			})
			if err != nil {
				t.Fatalf("new provider: %v", err)
			}
			defer closeProvider(t, instance)
			conversation := instance.NewConversation()

			runCompletedTurn(t, conversation, "first")
			runCompletedTurn(t, conversation, "second")

			mu.Lock()
			defer mu.Unlock()
			if len(requests) != 2 {
				t.Fatalf("request count: %d", len(requests))
			}
			if len(requests[0].Input) != 1 || requests[0].Input[0].Content[0].Text != "first" {
				t.Fatalf("first request input: %#v", requests[0].Input)
			}
			second := requests[1].Input
			if len(second) != 3 || second[0].Content[0].Text != "first" || second[1].ID != "msg-1" || second[2].Content[0].Text != "second" {
				t.Fatalf("second request input: %#v", second)
			}
		})
	}
}

func TestConversationProjectionMatchesLiveTextAndHidesActiveTurn(t *testing.T) {
	firstDeltaWritten := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello \"}\n\n")
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		close(firstDeltaWritten)
		<-release
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"world\"}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"reasoning\",\"id\":\"reasoning-live\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"private\"}],\"encrypted_content\":\"opaque\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"id\":\"msg-live\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello world\"}]}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-live\"}}\n\n")
	}))
	defer server.Close()

	instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("test-key"), Model: "gpt-test"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer closeProvider(t, instance)
	conversation := instance.NewConversation().(*Conversation)
	stream, err := conversation.Stream(context.Background(), provider.TurnInput{Text: "question"})
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	<-firstDeltaWritten

	firstEvent := <-stream
	liveText := decodeOpenAILiveText(t, firstEvent)
	if projection := conversation.ProjectHistory(); len(projection.Turns) != 0 {
		t.Fatalf("active turn projected: %#v", projection)
	}
	close(release)
	terminalCount := 0
	for event := range stream {
		liveText += decodeOpenAILiveText(t, event)
		if event.Kind.Terminal() {
			terminalCount++
			if event.Kind != provider.StreamEventCompleted {
				t.Fatalf("terminal: %#v", event)
			}
			if event.Prepared == nil {
				t.Fatal("completed terminal is missing prepared sample")
			}
			if err := event.Prepared.Finalize(); err != nil {
				t.Fatalf("finalize sample: %v", err)
			}
		}
	}
	if terminalCount != 1 {
		t.Fatalf("terminal count: %d", terminalCount)
	}
	projection := conversation.ProjectHistory()
	if len(projection.Turns) != 1 || projection.Turns[0].UserText != "question" || projection.Turns[0].AssistantText != liveText || liveText != "hello world" {
		t.Fatalf("live text %q projection %#v", liveText, projection)
	}
}

func decodeOpenAILiveText(t *testing.T, event provider.StreamEvent) string {
	t.Helper()
	if event.Kind != provider.StreamEventSemantic {
		return ""
	}
	payload, err := protocol.DecodeAssistantTextDelta(event.Event)
	if err != nil {
		t.Fatalf("decode assistant text delta: %v", err)
	}
	return payload.Text
}

func runCompletedTurn(t *testing.T, conversation provider.Conversation, text string) {
	t.Helper()
	stream, err := conversation.Stream(context.Background(), provider.TurnInput{Text: text})
	if err != nil {
		t.Fatalf("start turn %q: %v", text, err)
	}
	terminalCount := 0
	for event := range stream {
		if event.Kind.Terminal() {
			terminalCount++
			if event.Kind != provider.StreamEventCompleted || event.Err != nil {
				t.Fatalf("turn %q terminal: %#v", text, event)
			}
			if event.Prepared == nil {
				t.Fatalf("turn %q completed without prepared sample", text)
			}
			if err := event.Prepared.Finalize(); err != nil {
				t.Fatalf("turn %q finalize: %v", text, err)
			}
		}
	}
	if terminalCount != 1 {
		t.Fatalf("turn %q terminal count: %d", text, terminalCount)
	}
}
