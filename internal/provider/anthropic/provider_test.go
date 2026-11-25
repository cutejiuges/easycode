package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/provider"
	"easycode/internal/provider/testsupport"
	"easycode/internal/provider/transport"
	"easycode/internal/secret"
)

func TestConversationCancelCompletedRaceHasOneTerminal(t *testing.T) {
	for iteration := 0; iteration < 32; iteration++ {
		conversation := &Conversation{}
		ctx, cancel := context.WithCancel(context.Background())
		stream := make(chan transport.SSEMessage, 5)
		stream <- anthropicEvent(`{"type":"message_start","message":{"id":"msg-race","model":"claude-test"}}`)
		stream <- anthropicEvent(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"answer"}}`)
		stream <- anthropicEvent(`{"type":"content_block_stop","index":0}`)
		var senders sync.WaitGroup
		senders.Add(2)
		go func() {
			defer senders.Done()
			stream <- anthropicEvent(`{"type":"message_stop"}`)
		}()
		go func() {
			defer senders.Done()
			cancel()
			stream <- transport.SSEMessage{Err: context.Canceled}
		}()
		senders.Wait()
		close(stream)
		output := testConsumeStream(t, conversation, ctx, func() {}, nativeMessagePointer(newUserMessage("question")), testsupport.ToolCatalog(t), stream)
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

func anthropicEvent(data string) transport.SSEMessage {
	event := transport.SSEEvent{Data: data}
	return transport.SSEMessage{Event: &event}
}

func closeAnthropicProvider(t *testing.T, instance *Provider) {
	t.Helper()
	if err := instance.Close(); err != nil {
		t.Errorf("close provider: %v", err)
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
	conversation := testConversation(t, instance)
	events := runCompletedAnthropicTurn(t, conversation, "hello")
	wantKinds := []provider.StreamEventKind{provider.StreamEventSemantic, provider.StreamEventCompleted}
	if len(events) != len(wantKinds) {
		t.Fatalf("events: %#v", events)
	}
	for index, kind := range wantKinds {
		if events[index].Kind() != kind {
			t.Fatalf("event %d: got %s want %s", index, events[index].Kind(), kind)
		}
	}
	history := conversation.historySnapshot()
	if len(history) != 2 || history[0].UserInputs[0].Content[0].Text != "hello" || history[1].Assistant.Content[0].Text != "hello" {
		t.Fatalf("committed history: %#v", history)
	}
	if history[1].Metadata.ID != "msg-1" || history[1].Metadata.StopReason.Value != "end_turn" || history[1].Metadata.Usage.OutputTokens.Value != 2 {
		t.Fatalf("committed metadata: %#v", history[1].Metadata)
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
			conversation := testConversation(t, instance)
			stream, err := conversation.Stream(ctx, testAnthropicTurnInputWithProject(t, conversation, "hello"))
			if err != nil {
				t.Fatalf("start stream: %v", err)
			}
			if test.cancel {
				cancel()
			} else {
				defer cancel()
			}
			terminal := readAnthropicTerminal(t, conversation, stream)
			if terminal.Kind() != test.wantKind {
				t.Fatalf("terminal: %#v", terminal)
			}
			assertFaultCode(t, terminal.Error(), test.wantCode)
			if projection := conversation.ProjectHistory(); len(projection.Turns) != 1 || len(projection.Turns[0].Messages) != 1 || projection.Turns[0].Messages[0].Role != domain.SemanticUser {
				t.Fatalf("cancelled or timed out turn projected: %#v", projection)
			}
			if len(conversation.historySnapshot()) != 1 {
				t.Fatalf("cancelled or timed out turn committed: %#v", conversation.historySnapshot())
			}
			select {
			case <-requestDone:
			case <-time.After(time.Second):
				t.Fatal("request goroutine was not cleaned up")
			}
		})
	}
}

func testAnthropicTurnInputWithProject(t *testing.T, conversation provider.Conversation, text string) provider.TurnInput {
	t.Helper()
	testCommitUserInput(t, conversation.(*Conversation), text)
	snapshot := testAnthropicProjectInstructions(t, "AGENTS.md", "project-only-marker")
	input, err := testSamplingInput(t, conversation.(*Conversation).provider.config.Inference).WithProjectInstructions(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	input, err = input.WithToolCatalog(testsupport.ToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func readAnthropicTerminal(t *testing.T, conversation *Conversation, stream provider.StreamEvents) provider.StreamEvent {
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
	if conversation.operations.State()&provider.ConversationActive != 0 {
		t.Fatal("stream channel closed before conversation became inactive")
	}
	return terminal
}
