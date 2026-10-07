package anthropic

import (
	"bytes"
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
			var rawRequests [][]byte
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
				rawRequests = append(rawRequests, append([]byte(nil), body...))
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
			firstCompiled, err := compileMessagesRequest("claude-test", DefaultMaxOutputTokens, nil, nil, nativeMessagePointer(newUserMessage("first")), testAnthropicToolView(t))
			if err != nil {
				t.Fatalf("compile first expected request: %v", err)
			}
			secondCompiled, err := compileMessagesRequest("claude-test", DefaultMaxOutputTokens, []nativeHistoryEntry{{
				Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("first")),
				Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{
					{Type: blockTypeThinking, Thinking: "private", Signature: "opaque-signature"},
					{Type: blockTypeRedactedThinking, RedactedData: "opaque-data"},
					{Type: blockTypeText, Text: "answer"},
				}},
			}}, nil, nativeMessagePointer(newUserMessage("second")), testAnthropicToolView(t))
			if err != nil {
				t.Fatalf("compile second expected request: %v", err)
			}
			if !bytes.Equal(rawRequests[0], firstCompiled.Bytes()) || !bytes.Equal(rawRequests[1], secondCompiled.Bytes()) {
				t.Fatalf("HTTP body differs from compiler output:\nfirst=%s\nsecond=%s", rawRequests[0], rawRequests[1])
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

func TestConversationKeepsProjectInstructionsOutOfNativeHistory(t *testing.T) {
	const projectMarker = "project-only-marker"
	var mu sync.Mutex
	var requests []messagesRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
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
		mu.Unlock()
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSuccessfulAnthropicTurn(writer)
	}))
	defer server.Close()

	instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("test-key"), Model: "claude-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer closeAnthropicProvider(t, instance)
	conversation := instance.NewConversation().(*Conversation)
	snapshot := testAnthropicProjectInstructions(t, "AGENTS.md", projectMarker)
	for _, text := range []string{"first", "second"} {
		input, attachErr := (provider.TurnInput{Text: text}).WithProjectInstructions(snapshot)
		if attachErr != nil {
			t.Fatal(attachErr)
		}
		input, attachErr = input.WithToolCatalog(testAnthropicToolCatalog(t))
		if attachErr != nil {
			t.Fatal(attachErr)
		}
		stream, streamErr := conversation.Stream(context.Background(), input)
		if streamErr != nil {
			t.Fatal(streamErr)
		}
		for event := range stream {
			if event.Kind() != provider.StreamEventCompleted {
				continue
			}
			envelope, envelopeErr := event.PreparedSample().Envelope()
			if envelopeErr != nil {
				t.Fatal(envelopeErr)
			}
			if bytes.Contains(envelope.Payload(), []byte(projectMarker)) {
				t.Fatalf("native commit contains project instructions: %s", envelope.Payload())
			}
			if finalizeErr := event.PreparedSample().Finalize(); finalizeErr != nil {
				t.Fatal(finalizeErr)
			}
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || len(requests[0].Messages) != 2 || len(requests[1].Messages) != 4 {
		t.Fatalf("request messages: %#v", requests)
	}
	for index, request := range requests {
		encoded, err := codec.MarshalCanonical(request, maxMessagesRequestBytes)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Count(encoded.Bytes(), []byte(projectMarker)) != 1 {
			t.Fatalf("request %d does not contain exactly one project context: %s", index, encoded.Bytes())
		}
	}
	history := conversation.historySnapshot()
	projection := conversation.ProjectHistory()
	if len(history) != 2 || history[0].Input.Content[0].Text != "first" || history[1].Input.Content[0].Text != "second" {
		t.Fatalf("native history contains unexpected users: %#v", history)
	}
	if len(projection.Turns) != 2 || strings.Contains(projection.Turns[0].UserText, projectMarker) || strings.Contains(projection.Turns[1].UserText, projectMarker) {
		t.Fatalf("semantic history contains project instructions: %#v", projection)
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
	stream, err := conversation.Stream(context.Background(), testAnthropicTurnInput(t, "question"))
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
		if event.Kind().Terminal() {
			terminalCount++
			if event.Kind() != provider.StreamEventCompleted {
				t.Fatalf("terminal: %#v", event)
			}
			if event.PreparedSample() == nil {
				t.Fatal("completed terminal is missing prepared sample")
			}
			if err := event.PreparedSample().Finalize(); err != nil {
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
	if event.Kind() != provider.StreamEventSemantic {
		return ""
	}
	payload, err := protocol.DecodeAssistantTextDelta(event.Semantic())
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
	stream, err := conversation.Stream(context.Background(), testAnthropicTurnInput(t, text))
	if err != nil {
		t.Fatalf("start turn %q: %v", text, err)
	}
	var events []provider.StreamEvent
	terminalCount := 0
	terminalSeen := false
	for event := range stream {
		if err := event.Validate(); err != nil {
			t.Fatalf("turn %q invalid event: %v", text, err)
		}
		if terminalSeen {
			t.Fatalf("turn %q event after terminal: %s", text, event.Kind())
		}
		events = append(events, event)
		if event.Kind().Terminal() {
			terminalCount++
			terminalSeen = true
			if event.Kind() != provider.StreamEventCompleted || event.Error() != nil {
				t.Fatalf("turn %q terminal: %#v", text, event)
			}
			if event.PreparedSample() == nil {
				t.Fatalf("turn %q completed without prepared sample", text)
			}
			if got := len(conversation.ProjectHistory().Turns); got != committedBefore {
				t.Fatalf("turn %q committed before finalization: %d", text, got)
			}
			if err := event.PreparedSample().Finalize(); err != nil {
				t.Fatalf("turn %q finalize: %v", text, err)
			}
		}
	}
	if terminalCount != 1 {
		t.Fatalf("turn %q terminal count: %d", text, terminalCount)
	}
	if concrete, ok := conversation.(*Conversation); ok && concrete.active.Load() {
		t.Fatalf("turn %q channel closed before conversation became inactive", text)
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
	_, err = instance.NewConversation().Stream(context.Background(), testAnthropicTurnInput(t, "hello"))
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
