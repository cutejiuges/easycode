package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"easycode/internal/fault"
	"easycode/internal/provider"
	"easycode/internal/secret"
)

func TestProviderCreatesIsolatedConversations(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.com/v1", APIKey: secret.New("test-key"), Model: "gpt-test"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer closeProvider(t, instance)

	first := instance.NewConversation().(*Conversation)
	second := instance.NewConversation().(*Conversation)
	first.history.commit(NewUserItem("first"), []NativeItem{{Type: "message", ID: "msg-1", Role: "assistant"}})

	if len(first.historySnapshot()) != 2 {
		t.Fatalf("first history: %#v", first.historySnapshot())
	}
	if len(second.historySnapshot()) != 0 {
		t.Fatalf("second conversation shared history: %#v", second.historySnapshot())
	}
}

func TestProviderCapabilitiesReflectImplementedSlice(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.com/v1", APIKey: secret.New("test-key"), Model: "gpt-test"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer closeProvider(t, instance)

	capabilities := instance.Capabilities()
	if !capabilities.Streaming || !capabilities.EncryptedReasoning {
		t.Fatalf("implemented capabilities missing: %#v", capabilities)
	}
	if capabilities.FunctionTools || capabilities.ParallelToolCalls || capabilities.PromptCacheKey || capabilities.PreviousResponse || capabilities.ReasoningSummary {
		t.Fatalf("unimplemented capabilities advertised: %#v", capabilities)
	}
}

func TestConversationStreamsAndCommitsOnlyOnCompleted(t *testing.T) {
	authorization := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorization <- request.Header.Get("Authorization")
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"id\":\"msg-1\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\"}}\n\n")
	}))
	defer server.Close()

	instance, err := New(Config{BaseURL: server.URL + "/v1", APIKey: secret.New("test-key"), Model: "gpt-test"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer closeProvider(t, instance)
	conversation := instance.NewConversation().(*Conversation)

	stream, err := conversation.Stream(context.Background(), provider.TurnInput{Text: "hello"})
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	var kinds []provider.StreamEventKind
	for event := range stream {
		kinds = append(kinds, event.Kind)
	}
	want := []provider.StreamEventKind{provider.StreamEventSemantic, provider.StreamEventNative, provider.StreamEventCompleted}
	if len(kinds) != len(want) {
		t.Fatalf("stream kinds: got %#v want %#v", kinds, want)
	}
	for index := range want {
		if kinds[index] != want[index] {
			t.Fatalf("stream kind %d: got %s want %s", index, kinds[index], want[index])
		}
	}
	if got := <-authorization; got != "Bearer test-key" {
		t.Fatalf("authorization: %q", got)
	}
	if history := conversation.historySnapshot(); len(history) != 2 || history[1].ID != "msg-1" {
		t.Fatalf("committed history: %#v", history)
	}
}

func TestConversationRejectsEOFBeforeCompletedWithoutCommitting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
	}))
	defer server.Close()

	instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("test-key"), Model: "gpt-test"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer closeProvider(t, instance)
	conversation := instance.NewConversation().(*Conversation)

	stream, err := conversation.Stream(context.Background(), provider.TurnInput{Text: "hello"})
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	var terminal provider.StreamEvent
	for event := range stream {
		if event.Kind.Terminal() {
			terminal = event
		}
	}
	if terminal.Kind != provider.StreamEventFailed {
		t.Fatalf("terminal: %#v", terminal)
	}
	var faultError *fault.Error
	if !errors.As(terminal.Err, &faultError) || faultError.Code != fault.CodeStreamProtocol {
		t.Fatalf("terminal error: %v", terminal.Err)
	}
	if len(conversation.historySnapshot()) != 0 {
		t.Fatalf("partial turn committed: %#v", conversation.historySnapshot())
	}
}

func TestConversationCancelProducesCancelledTerminal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		<-request.Context().Done()
	}))
	defer server.Close()

	instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("test-key"), Model: "gpt-test"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer closeProvider(t, instance)
	conversation := instance.NewConversation().(*Conversation)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := conversation.Stream(ctx, provider.TurnInput{Text: "hello"})
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	cancel()

	var terminal provider.StreamEvent
	for event := range stream {
		if event.Kind.Terminal() {
			terminal = event
		}
	}
	if terminal.Kind != provider.StreamEventCancelled || !errors.Is(terminal.Err, context.Canceled) {
		t.Fatalf("terminal: %#v", terminal)
	}
}

func TestProviderRequestErrorDoesNotExposeAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("top-secret"), Model: "gpt-test"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer closeProvider(t, instance)
	_, err = instance.NewConversation().Stream(context.Background(), provider.TurnInput{Text: "hello"})
	if err == nil || strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("unsafe provider error: %v", err)
	}
}

func closeProvider(t *testing.T, instance *Provider) {
	t.Helper()
	if err := instance.Close(); err != nil {
		t.Errorf("close provider: %v", err)
	}
}
