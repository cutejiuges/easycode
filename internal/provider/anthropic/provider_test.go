package anthropic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/fault"
	"easycode/internal/provider"
	"easycode/internal/provider/transport"
	"easycode/internal/secret"
)

func TestNewProviderValidatesAndNormalizesConfiguration(t *testing.T) {
	tests := []struct {
		name       string
		config     Config
		wantCode   fault.Code
		wantTokens int
	}{
		{
			name:       "default output tokens",
			config:     validTestConfig(),
			wantTokens: DefaultMaxOutputTokens,
		},
		{
			name: "explicit output tokens",
			config: Config{
				BaseURL:         "https://example.com/v1",
				APIKey:          secret.New("test-key"),
				Model:           " claude-test ",
				MaxOutputTokens: 8192,
			},
			wantTokens: 8192,
		},
		{
			name:     "missing model",
			config:   Config{BaseURL: "https://example.com", APIKey: secret.New("test-key")},
			wantCode: fault.CodeInvalidConfiguration,
		},
		{
			name:     "missing API key",
			config:   Config{BaseURL: "https://example.com", APIKey: secret.New("  "), Model: "claude-test"},
			wantCode: fault.CodeInvalidConfiguration,
		},
		{
			name:     "invalid base URL",
			config:   Config{BaseURL: "not-a-url", APIKey: secret.New("top-secret"), Model: "claude-test"},
			wantCode: fault.CodeInvalidConfiguration,
		},
		{
			name:     "negative output tokens",
			config:   Config{BaseURL: "https://example.com", APIKey: secret.New("test-key"), Model: "claude-test", MaxOutputTokens: -1},
			wantCode: fault.CodeInvalidConfiguration,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, err := New(test.config)
			if test.wantCode != "" {
				var faultError *fault.Error
				if !errors.As(err, &faultError) || faultError.Code != test.wantCode {
					t.Fatalf("new provider error: got %v want code %s", err, test.wantCode)
				}
				if strings.Contains(err.Error(), "top-secret") || strings.Contains(err.Error(), "test-key") {
					t.Fatalf("configuration error leaked API key: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("new provider: %v", err)
			}
			defer closeAnthropicProvider(t, instance)
			if instance.config.MaxOutputTokens != test.wantTokens || instance.config.Model != "claude-test" {
				t.Fatalf("normalized config: %#v", instance.config)
			}
		})
	}
}

func validTestConfig() Config {
	return Config{
		BaseURL: "https://example.com/v1",
		APIKey:  secret.New("test-key"),
		Model:   "claude-test",
	}
}

func closeAnthropicProvider(t *testing.T, instance *Provider) {
	t.Helper()
	if err := instance.Close(); err != nil {
		t.Errorf("close provider: %v", err)
	}
}

func TestProviderCreatesIsolatedConversations(t *testing.T) {
	instance, err := New(validTestConfig())
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer closeAnthropicProvider(t, instance)

	first := instance.NewConversation().(*Conversation)
	second := instance.NewConversation().(*Conversation)
	first.history.commit(nativeTurn{
		User:      newUserMessage("first"),
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeText, Text: "answer"}}},
	})

	if len(first.historySnapshot()) != 1 {
		t.Fatalf("first history: %#v", first.historySnapshot())
	}
	if len(second.historySnapshot()) != 0 {
		t.Fatalf("second conversation shared history: %#v", second.historySnapshot())
	}
}

func TestProviderCapabilitiesReflectImplementedSlice(t *testing.T) {
	instance, err := New(validTestConfig())
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer closeAnthropicProvider(t, instance)

	want := provider.Capabilities{Streaming: true, ThinkingSignature: true}
	if got := instance.Capabilities(); got != want {
		t.Fatalf("capabilities: got %#v want %#v", got, want)
	}
}

func TestConversationAllowsOnlyOneActiveTurn(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		close(requestStarted)
		<-request.Context().Done()
	}))
	defer server.Close()

	instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("test-key"), Model: "claude-test"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer closeAnthropicProvider(t, instance)
	conversation := instance.NewConversation()
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := conversation.Stream(ctx, provider.TurnInput{Text: "first"})
	if err != nil {
		t.Fatalf("start first turn: %v", err)
	}
	<-requestStarted

	second, err := conversation.Stream(context.Background(), provider.TurnInput{Text: "second"})
	if second != nil {
		t.Fatalf("concurrent turn returned stream: %#v", second)
	}
	assertFaultCode(t, err, fault.CodeTurnFailed)

	cancel()
	terminal := readAnthropicTerminal(t, stream)
	if terminal.Kind != provider.StreamEventCancelled || !errors.Is(terminal.Err, context.Canceled) {
		t.Fatalf("cancel terminal: %#v", terminal)
	}
}

func TestConversationPublishesOrderedEventsAndCommitsOnMessageStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-1\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":3}}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"hello\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("test-key"), Model: "claude-test"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer closeAnthropicProvider(t, instance)
	conversation := instance.NewConversation().(*Conversation)
	events := runCompletedAnthropicTurn(t, conversation, "hello")
	wantKinds := []provider.StreamEventKind{provider.StreamEventSemantic, provider.StreamEventNative, provider.StreamEventCompleted}
	if len(events) != len(wantKinds) {
		t.Fatalf("events: %#v", events)
	}
	for index, kind := range wantKinds {
		if events[index].Kind != kind {
			t.Fatalf("event %d: got %s want %s", index, events[index].Kind, kind)
		}
	}
	history := conversation.historySnapshot()
	if len(history) != 1 || history[0].User.Content[0].Text != "hello" || history[0].Assistant.Content[0].Text != "hello" {
		t.Fatalf("committed history: %#v", history)
	}
	if history[0].Metadata.ID != "msg-1" || history[0].Metadata.StopReason.Value != "end_turn" || history[0].Metadata.Usage.OutputTokens.Value != 2 {
		t.Fatalf("committed metadata: %#v", history[0].Metadata)
	}
}

func TestConversationDiscardsFailedStagingBeforeNextTurn(t *testing.T) {
	var mu sync.Mutex
	var requests []messagesRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			t.Errorf("read request: %v", readErr)
			return
		}
		var decoded messagesRequest
		if decodeErr := codec.Unmarshal(body, &decoded); decodeErr != nil {
			t.Errorf("decode request: %v", decodeErr)
			return
		}
		mu.Lock()
		requests = append(requests, decoded)
		turn := len(requests)
		mu.Unlock()
		writer.Header().Set("Content-Type", "text/event-stream")
		if turn == 1 {
			_, _ = io.WriteString(writer, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"failed\",\"model\":\"claude-test\"}}\n\n")
			_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
			_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n")
			_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
			return
		}
		writeSuccessfulAnthropicTurn(writer)
	}))
	defer server.Close()

	instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("test-key"), Model: "claude-test"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer closeAnthropicProvider(t, instance)
	conversation := instance.NewConversation().(*Conversation)
	failedStream, err := conversation.Stream(context.Background(), provider.TurnInput{Text: "failed user"})
	if err != nil {
		t.Fatalf("start failed turn: %v", err)
	}
	failedTerminal := readAnthropicTerminal(t, failedStream)
	if failedTerminal.Kind != provider.StreamEventFailed {
		t.Fatalf("failed terminal: %#v", failedTerminal)
	}
	assertFaultCode(t, failedTerminal.Err, fault.CodeStreamProtocol)
	if len(conversation.historySnapshot()) != 0 {
		t.Fatalf("failed turn committed: %#v", conversation.historySnapshot())
	}

	runCompletedAnthropicTurn(t, conversation, "second")
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || len(requests[1].Messages) != 1 || requests[1].Messages[0].Content[0].Text != "second" {
		t.Fatalf("second request contains failed staging: %#v", requests)
	}
}

func TestConversationMapsStreamFailuresWithoutCommitting(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode fault.Code
	}{
		{
			name:     "provider error",
			body:     "data: {\"type\":\"error\",\"error\":{\"message\":\"sensitive response\"}}\n\n",
			wantCode: fault.CodeProviderRequest,
		},
		{
			name:     "invalid reducer state",
			body:     "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"x\"}}\n\n",
			wantCode: fault.CodeStreamProtocol,
		},
		{
			name:     "EOF before message stop",
			body:     "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-1\",\"model\":\"claude-test\"}}\n\n",
			wantCode: fault.CodeStreamProtocol,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()
			instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("test-key"), Model: "claude-test"})
			if err != nil {
				t.Fatalf("new provider: %v", err)
			}
			defer closeAnthropicProvider(t, instance)
			conversation := instance.NewConversation().(*Conversation)
			stream, err := conversation.Stream(context.Background(), provider.TurnInput{Text: "hello"})
			if err != nil {
				t.Fatalf("start stream: %v", err)
			}
			terminal := readAnthropicTerminal(t, stream)
			if terminal.Kind != provider.StreamEventFailed {
				t.Fatalf("terminal: %#v", terminal)
			}
			assertFaultCode(t, terminal.Err, test.wantCode)
			if strings.Contains(terminal.Err.Error(), "sensitive response") {
				t.Fatalf("terminal leaked provider body: %v", terminal.Err)
			}
			if len(conversation.historySnapshot()) != 0 {
				t.Fatalf("failed stream committed history: %#v", conversation.historySnapshot())
			}
		})
	}
}

func TestConversationCancelAndIdleTimeoutCleanUpRequest(t *testing.T) {
	tests := []struct {
		name     string
		cancel   bool
		wantKind provider.StreamEventKind
		wantCode fault.Code
	}{
		{name: "cancel", cancel: true, wantKind: provider.StreamEventCancelled, wantCode: fault.CodeUserCancelled},
		{name: "idle timeout", wantKind: provider.StreamEventFailed, wantCode: fault.CodeStreamIdleTimeout},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestDone := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "text/event-stream")
				if flusher, ok := writer.(http.Flusher); ok {
					flusher.Flush()
				}
				<-request.Context().Done()
				close(requestDone)
			}))
			defer server.Close()
			instance, err := New(Config{
				BaseURL:           server.URL,
				APIKey:            secret.New("test-key"),
				Model:             "claude-test",
				StreamIdleTimeout: 30 * time.Millisecond,
			})
			if err != nil {
				t.Fatalf("new provider: %v", err)
			}
			defer closeAnthropicProvider(t, instance)
			ctx, cancel := context.WithCancel(context.Background())
			stream, err := instance.NewConversation().Stream(ctx, provider.TurnInput{Text: "hello"})
			if err != nil {
				t.Fatalf("start stream: %v", err)
			}
			if test.cancel {
				cancel()
			} else {
				defer cancel()
			}
			terminal := readAnthropicTerminal(t, stream)
			if terminal.Kind != test.wantKind {
				t.Fatalf("terminal: %#v", terminal)
			}
			assertFaultCode(t, terminal.Err, test.wantCode)
			select {
			case <-requestDone:
			case <-time.After(time.Second):
				t.Fatal("request goroutine was not cleaned up")
			}
		})
	}
}

func TestConversationRejectsOversizedEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: ")
		_, _ = io.WriteString(writer, strings.Repeat("x", transport.DefaultMaxEventBytes))
		_, _ = io.WriteString(writer, "\n\n")
	}))
	defer server.Close()
	instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("test-key"), Model: "claude-test"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer closeAnthropicProvider(t, instance)
	conversation := instance.NewConversation().(*Conversation)
	stream, err := conversation.Stream(context.Background(), provider.TurnInput{Text: "hello"})
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	terminal := readAnthropicTerminal(t, stream)
	if terminal.Kind != provider.StreamEventFailed {
		t.Fatalf("terminal: %#v", terminal)
	}
	assertFaultCode(t, terminal.Err, fault.CodeStreamProtocol)
	if len(conversation.historySnapshot()) != 0 {
		t.Fatalf("oversized event committed history: %#v", conversation.historySnapshot())
	}
}

func readAnthropicTerminal(t *testing.T, stream <-chan provider.StreamEvent) provider.StreamEvent {
	t.Helper()
	var terminal provider.StreamEvent
	count := 0
	for event := range stream {
		if event.Kind.Terminal() {
			terminal = event
			count++
		}
	}
	if count != 1 {
		t.Fatalf("terminal count: %d", count)
	}
	return terminal
}
