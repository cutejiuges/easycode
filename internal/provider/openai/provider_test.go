package openai

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

	"easycode/internal/fault"
	"easycode/internal/provider"
	"easycode/internal/provider/transport"
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
	first.history.commit(testSampleEntry(NewUserItem("first"), []NativeItem{{Type: "message", ID: "msg-1", Role: "assistant"}}))

	if history := first.historySnapshot(); len(history) != 1 || history[0].Input.Content[0].Text != "first" || history[0].Outputs[0].ID != "msg-1" {
		t.Fatalf("first history: %#v", first.historySnapshot())
	}
	if len(second.historySnapshot()) != 0 {
		t.Fatalf("second conversation shared history: %#v", second.historySnapshot())
	}
	if got := first.ProjectHistory(); len(got.Turns) != 1 || got.Turns[0].UserText != "first" || got.Turns[0].AssistantText != "" {
		t.Fatalf("first projection: %#v", got)
	}
	if got := second.ProjectHistory(); got.Turns == nil || len(got.Turns) != 0 {
		t.Fatalf("second conversation shared projection: %#v", got)
	}
}

func TestConversationIgnoresRepeatedCompletedTerminal(t *testing.T) {
	t.Parallel()
	conversation := &Conversation{}
	stream := make(chan transport.SSEMessage, 4)
	stream <- responseEvent(`{"type":"response.created","response":{"id":"resp-1"}}`)
	stream <- responseEvent(`{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}}`)
	stream <- responseEvent(`{"type":"response.completed","response":{"id":"resp-1"}}`)
	stream <- responseEvent(`{"type":"response.completed","response":{"id":"resp-2"}}`)
	close(stream)
	output := make(chan provider.StreamEvent, 16)
	conversation.consumeStream(context.Background(), func() {}, nativeItemPointer(NewUserItem("question")), testOpenAIToolCatalog(t), stream, output)
	terminalCount := 0
	for event := range output {
		if !event.Kind().Terminal() {
			continue
		}
		terminalCount++
		if event.Kind() != provider.StreamEventCompleted || event.PreparedSample() == nil {
			t.Fatalf("terminal = %#v", event)
		}
		if err := event.PreparedSample().Finalize(); err != nil {
			t.Fatal(err)
		}
	}
	if terminalCount != 1 || len(conversation.historySnapshot()) != 1 {
		t.Fatalf("terminal count/history = %d/%#v", terminalCount, conversation.historySnapshot())
	}
}

func TestConversationCancelCompletedRaceHasOneTerminal(t *testing.T) {
	for iteration := 0; iteration < 32; iteration++ {
		conversation := &Conversation{}
		ctx, cancel := context.WithCancel(context.Background())
		stream := make(chan transport.SSEMessage, 4)
		stream <- responseEvent(`{"type":"response.created","response":{"id":"resp-race"}}`)
		stream <- responseEvent(`{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}}`)
		var senders sync.WaitGroup
		senders.Add(2)
		go func() {
			defer senders.Done()
			stream <- responseEvent(`{"type":"response.completed","response":{"id":"resp-race"}}`)
		}()
		go func() {
			defer senders.Done()
			cancel()
			stream <- transport.SSEMessage{Err: context.Canceled}
		}()
		senders.Wait()
		close(stream)
		output := make(chan provider.StreamEvent, 16)
		conversation.consumeStream(ctx, func() {}, nativeItemPointer(NewUserItem("question")), testOpenAIToolCatalog(t), stream, output)
		terminalCount := 0
		for event := range output {
			if !event.Kind().Terminal() {
				continue
			}
			terminalCount++
			switch event.Kind() {
			case provider.StreamEventCompleted:
				if event.PreparedSample() == nil {
					t.Fatal("completed race terminal has no prepared sample")
				}
				if err := event.PreparedSample().Finalize(); err != nil {
					t.Fatal(err)
				}
			case provider.StreamEventCancelled:
				if event.PreparedSample() != nil {
					t.Fatal("cancelled race terminal carried a prepared sample")
				}
			default:
				t.Fatalf("race terminal = %#v", event)
			}
		}
		if terminalCount != 1 {
			t.Fatalf("iteration %d terminal count = %d", iteration, terminalCount)
		}
	}
}

func responseEvent(data string) transport.SSEMessage {
	event := transport.SSEEvent{Data: data}
	return transport.SSEMessage{Event: &event}
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
	if !capabilities.FunctionTools || capabilities.ParallelToolCalls || capabilities.PromptCacheKey || capabilities.PreviousResponse || capabilities.ReasoningSummary {
		t.Fatalf("unimplemented capabilities advertised: %#v", capabilities)
	}
}

func TestConversationStreamsAndCommitsOnlyOnCompleted(t *testing.T) {
	authorization := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorization <- request.Header.Get("Authorization")
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-1\"}}\n\n")
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

	stream, err := conversation.Stream(context.Background(), testOpenAITurnInput(t, "hello"))
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	var kinds []provider.StreamEventKind
	var prepared *provider.PreparedSample
	for event := range stream {
		kinds = append(kinds, event.Kind())
		if event.Kind() == provider.StreamEventCompleted {
			prepared = event.PreparedSample()
		}
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
	if history := conversation.historySnapshot(); len(history) != 0 {
		t.Fatalf("history committed before durable finalization: %#v", history)
	}
	if prepared == nil {
		t.Fatal("completed terminal did not carry a prepared sample")
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Finalize(); err == nil {
		t.Fatal("prepared sample finalized twice")
	}
	if history := conversation.historySnapshot(); len(history) != 1 || history[0].Input.Content[0].Text != "hello" || history[0].Outputs[0].ID != "msg-1" {
		t.Fatalf("committed history: %#v", history)
	}
}

func TestConversationRejectsEOFBeforeCompletedWithoutCommitting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-1\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
	}))
	defer server.Close()

	instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("test-key"), Model: "gpt-test"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer closeProvider(t, instance)
	conversation := instance.NewConversation().(*Conversation)

	stream, err := conversation.Stream(context.Background(), testOpenAITurnInput(t, "hello"))
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	terminal := readOpenAITerminal(t, conversation, stream)
	if terminal.Kind() != provider.StreamEventFailed {
		t.Fatalf("terminal: %#v", terminal)
	}
	var faultError *fault.Error
	if !errors.As(terminal.Error(), &faultError) || faultError.Code != fault.CodeStreamProtocol {
		t.Fatalf("terminal error: %v", terminal.Error())
	}
	if len(conversation.historySnapshot()) != 0 {
		t.Fatalf("partial turn committed: %#v", conversation.historySnapshot())
	}
	if projection := conversation.ProjectHistory(); len(projection.Turns) != 0 {
		t.Fatalf("partial turn projected: %#v", projection)
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
	stream, err := conversation.Stream(ctx, testOpenAITurnInputWithProject(t, "hello"))
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	cancel()

	terminal := readOpenAITerminal(t, conversation, stream)
	if terminal.Kind() != provider.StreamEventCancelled || !errors.Is(terminal.Error(), context.Canceled) {
		t.Fatalf("terminal: %#v", terminal)
	}
	if projection := conversation.ProjectHistory(); len(projection.Turns) != 0 {
		t.Fatalf("cancelled turn projected: %#v", projection)
	}
	if len(conversation.historySnapshot()) != 0 {
		t.Fatalf("cancelled turn committed: %#v", conversation.historySnapshot())
	}
	nextContext, cancelNext := context.WithCancel(context.Background())
	cancelNext()
	if next, nextErr := conversation.Stream(nextContext, testOpenAITurnInput(t, "next")); next != nil || !errors.Is(nextErr, context.Canceled) {
		t.Fatalf("next stream after cleanup = %#v, %v", next, nextErr)
	}
}

func TestConversationFailureAndIdleTimeoutDoNotProjectTurn(t *testing.T) {
	tests := []struct {
		name        string
		writeStream func(http.ResponseWriter, *http.Request)
		wantCode    fault.Code
	}{
		{
			name: "failed response",
			writeStream: func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-failed\"}}\n\n")
				_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
				_, _ = io.WriteString(writer, "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp-failed\",\"error\":{\"code\":\"rate_limit_exceeded\"}}}\n\n")
			},
			wantCode: fault.CodeProviderRequest,
		},
		{
			name: "idle timeout",
			writeStream: func(writer http.ResponseWriter, request *http.Request) {
				if flusher, ok := writer.(http.Flusher); ok {
					flusher.Flush()
				}
				<-request.Context().Done()
			},
			wantCode: fault.CodeStreamIdleTimeout,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "text/event-stream")
				test.writeStream(writer, request)
			}))
			defer server.Close()

			instance, err := New(Config{
				BaseURL:           server.URL,
				APIKey:            secret.New("test-key"),
				Model:             "gpt-test",
				StreamIdleTimeout: 30 * time.Millisecond,
			})
			if err != nil {
				t.Fatalf("new provider: %v", err)
			}
			defer closeProvider(t, instance)
			conversation := instance.NewConversation().(*Conversation)
			stream, err := conversation.Stream(context.Background(), testOpenAITurnInputWithProject(t, "hello"))
			if err != nil {
				t.Fatalf("start stream: %v", err)
			}
			terminal := readOpenAITerminal(t, conversation, stream)
			if terminal.Kind() != provider.StreamEventFailed {
				t.Fatalf("terminal: %#v", terminal)
			}
			var faultError *fault.Error
			if !errors.As(terminal.Error(), &faultError) || faultError.Code != test.wantCode {
				t.Fatalf("terminal error: got %v want code %s", terminal.Error(), test.wantCode)
			}
			if projection := conversation.ProjectHistory(); len(projection.Turns) != 0 {
				t.Fatalf("failed turn projected: %#v", projection)
			}
			if len(conversation.historySnapshot()) != 0 {
				t.Fatalf("failed turn committed: %#v", conversation.historySnapshot())
			}
		})
	}
}

func testOpenAITurnInputWithProject(t *testing.T, text string) provider.TurnInput {
	t.Helper()
	snapshot := testOpenAIProjectInstructions(t, "AGENTS.md", "project-only-marker")
	input, err := (provider.TurnInput{Text: text}).WithProjectInstructions(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	input, err = input.WithToolCatalog(testOpenAIToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	return input
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
	_, err = instance.NewConversation().Stream(context.Background(), testOpenAITurnInput(t, "hello"))
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

func readOpenAITerminal(t *testing.T, conversation *Conversation, stream <-chan provider.StreamEvent) provider.StreamEvent {
	t.Helper()
	var terminal provider.StreamEvent
	terminalSeen := false
	for event := range stream {
		if err := event.Validate(); err != nil {
			t.Fatalf("invalid stream event: %v", err)
		}
		if terminalSeen {
			t.Fatalf("event after terminal: %s", event.Kind())
		}
		if event.Kind().Terminal() {
			terminal = event
			terminalSeen = true
		}
	}
	if !terminalSeen {
		t.Fatal("stream closed without terminal")
	}
	if conversation.active.Load() {
		t.Fatal("stream channel closed before conversation became inactive")
	}
	return terminal
}
