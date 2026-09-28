package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
		{name: "host only", wantPath: "/messages"},
		{name: "path prefix", prefix: "/anthropic/v1/", wantPath: "/anthropic/v1/messages"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var mu sync.Mutex
			var requests []messagesRequest
			var methods []string
			var headers []http.Header
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != test.wantPath {
					t.Errorf("request path: got %q want %q", request.URL.Path, test.wantPath)
				}
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Errorf("read request: %v", err)
					return
				}
				var decoded messagesRequest
				if err := codec.Unmarshal(body, &decoded); err != nil {
					t.Errorf("decode request: %v", err)
					return
				}
				mu.Lock()
				requests = append(requests, decoded)
				methods = append(methods, request.Method)
				headers = append(headers, request.Header.Clone())
				mu.Unlock()

				writer.Header().Set("Content-Type", "text/event-stream")
				writeSuccessfulAnthropicTurn(writer)
			}))
			defer server.Close()

			instance, err := New(Config{
				BaseURL: server.URL + test.prefix,
				APIKey:  secret.New("test-key"),
				Model:   "claude-test",
			})
			if err != nil {
				t.Fatalf("new provider: %v", err)
			}
			defer closeAnthropicProvider(t, instance)
			conversation := instance.NewConversation()

			runCompletedAnthropicTurn(t, conversation, "first")
			runCompletedAnthropicTurn(t, conversation, "second")

			mu.Lock()
			defer mu.Unlock()
			if len(requests) != 2 {
				t.Fatalf("request count: %d", len(requests))
			}
			for index := range requests {
				if methods[index] != http.MethodPost {
					t.Fatalf("request method: %q", methods[index])
				}
				if headers[index].Get("x-api-key") != "test-key" || headers[index].Get("anthropic-version") != apiVersion {
					t.Fatalf("Anthropic headers: %#v", headers[index])
				}
				if headers[index].Get("Accept") != "text/event-stream" || headers[index].Get("Content-Type") != "application/json" {
					t.Fatalf("content headers: %#v", headers[index])
				}
			}
			if len(requests[0].Messages) != 1 || requests[0].Messages[0].Content[0].Text != "first" {
				t.Fatalf("first request: %#v", requests[0])
			}
			second := requests[1]
			if second.Model != "claude-test" || second.MaxTokens != DefaultMaxOutputTokens || !second.Stream || len(second.Messages) != 3 {
				t.Fatalf("second request shape: %#v", second)
			}
			if second.Messages[0].Role != roleUser || second.Messages[0].Content[0].Text != "first" || second.Messages[2].Content[0].Text != "second" {
				t.Fatalf("second request user history: %#v", second.Messages)
			}
			assistant := second.Messages[1]
			if assistant.Role != roleAssistant || len(assistant.Content) != 3 {
				t.Fatalf("assistant history: %#v", assistant)
			}
			if assistant.Content[0].Thinking != "private" || assistant.Content[0].Signature != "opaque-signature" {
				t.Fatalf("thinking history: %#v", assistant.Content[0])
			}
			if assistant.Content[1].RedactedData != "opaque-data" || assistant.Content[2].Text != "answer" {
				t.Fatalf("assistant block order: %#v", assistant.Content)
			}
		})
	}
}

func TestConversationProjectionMatchesLiveTextAndHidesActiveTurn(t *testing.T) {
	firstDeltaWritten := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-live\",\"model\":\"claude-test\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello \"}}\n\n")
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		close(firstDeltaWritten)
		<-release
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"world\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("test-key"), Model: "claude-test"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer closeAnthropicProvider(t, instance)
	conversation := instance.NewConversation().(*Conversation)
	stream, err := conversation.Stream(context.Background(), provider.TurnInput{Text: "question"})
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	<-firstDeltaWritten

	firstEvent := <-stream
	liveText := decodeAnthropicLiveText(t, firstEvent)
	if projection := conversation.ProjectHistory(); len(projection.Turns) != 0 {
		t.Fatalf("active turn projected: %#v", projection)
	}
	close(release)
	terminalCount := 0
	for event := range stream {
		liveText += decodeAnthropicLiveText(t, event)
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

func decodeAnthropicLiveText(t *testing.T, event provider.StreamEvent) string {
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

func writeSuccessfulAnthropicTurn(writer io.Writer) {
	_, _ = io.WriteString(writer, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-1\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":5}}}\n\n")
	_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}\n\n")
	_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"private\"}}\n\n")
	_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"opaque-signature\"}}\n\n")
	_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
	_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"redacted_thinking\",\"data\":\"opaque-data\"}}\n\n")
	_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_stop\",\"index\":1}\n\n")
	_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_start\",\"index\":2,\"content_block\":{\"type\":\"text\",\"text\":\"answer\"}}\n\n")
	_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_delta\",\"index\":2,\"delta\":{\"type\":\"text_delta\",\"text\":\"answer\"}}\n\n")
	_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_stop\",\"index\":2}\n\n")
	_, _ = io.WriteString(writer, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\n")
	_, _ = io.WriteString(writer, "data: {\"type\":\"message_stop\"}\n\n")
}

func runCompletedAnthropicTurn(t *testing.T, conversation provider.Conversation, text string) []provider.StreamEvent {
	t.Helper()
	committedBefore := len(conversation.ProjectHistory().Turns)
	stream, err := conversation.Stream(context.Background(), provider.TurnInput{Text: text})
	if err != nil {
		t.Fatalf("start turn %q: %v", text, err)
	}
	var events []provider.StreamEvent
	terminalCount := 0
	for event := range stream {
		events = append(events, event)
		if event.Kind.Terminal() {
			terminalCount++
			if event.Kind != provider.StreamEventCompleted || event.Err != nil {
				t.Fatalf("turn %q terminal: %#v", text, event)
			}
			if event.Prepared == nil {
				t.Fatalf("turn %q completed without prepared sample", text)
			}
			if got := len(conversation.ProjectHistory().Turns); got != committedBefore {
				t.Fatalf("turn %q committed before finalization: %d", text, got)
			}
			if err := event.Prepared.Finalize(); err != nil {
				t.Fatalf("turn %q finalize: %v", text, err)
			}
		}
	}
	if terminalCount != 1 {
		t.Fatalf("turn %q terminal count: %d", text, terminalCount)
	}
	if got := len(conversation.ProjectHistory().Turns); got != committedBefore+1 {
		t.Fatalf("turn %q committed turns: %d", text, got)
	}
	return events
}

func TestProviderRequestErrorIsSanitizedAndNotRetried(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(writer, "sensitive-response-body")
	}))
	defer server.Close()

	instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("top-secret"), Model: "claude-test"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer closeAnthropicProvider(t, instance)
	_, err = instance.NewConversation().Stream(context.Background(), provider.TurnInput{Text: "hello"})
	if err == nil {
		t.Fatal("expected provider request error")
	}
	if requests != 1 {
		t.Fatalf("request was retried: %d", requests)
	}
	for _, sensitive := range []string{"top-secret", "sensitive-response-body", "x-api-key"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("provider error leaked %q: %v", sensitive, err)
		}
	}
}
