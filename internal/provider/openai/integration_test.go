package openai

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/secret"
)

func TestConversationStreamIdentityIntegration(t *testing.T) {
	tests := []struct {
		name      string
		events    []string
		wantKind  provider.StreamEventKind
		wantFault fault.Code
	}{
		{
			name: "conflicting terminal identity",
			events: []string{
				`{"type":"response.created","response":{"id":"resp-1"}}`,
				`{"type":"response.completed","response":{"id":"resp-2"}}`,
			},
			wantKind:  provider.StreamEventFailed,
			wantFault: fault.CodeStreamProtocol,
		},
		{
			name: "event before created",
			events: []string{
				`{"type":"response.output_text.delta","delta":"early"}`,
			},
			wantKind:  provider.StreamEventFailed,
			wantFault: fault.CodeStreamProtocol,
		},
		{
			name: "unknown active event",
			events: []string{
				`{"type":"response.created","response":{"id":"resp-1"}}`,
				`{"type":"response.future_event","future":{"enabled":true}}`,
				`{"type":"response.output_item.done","item":{"type":"message","id":"msg-1","role":"assistant","content":[{"type":"output_text","text":"answer"}]}}`,
				`{"type":"response.completed","response":{"id":"resp-1"}}`,
			},
			wantKind: provider.StreamEventCompleted,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "text/event-stream")
				for _, event := range test.events {
					_, _ = io.WriteString(writer, "data: "+event+"\n\n")
				}
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

			var terminal provider.StreamEvent
			for event := range stream {
				if event.Kind.Terminal() {
					terminal = event
				}
			}
			if terminal.Kind != test.wantKind {
				t.Fatalf("terminal kind: got %s want %s", terminal.Kind, test.wantKind)
			}
			if test.wantFault != "" {
				var faultError *fault.Error
				if !errors.As(terminal.Err, &faultError) || faultError.Code != test.wantFault {
					t.Fatalf("terminal error: got %v want code %s", terminal.Err, test.wantFault)
				}
				if len(conversation.historySnapshot()) != 0 {
					t.Fatalf("invalid stream committed history: %#v", conversation.historySnapshot())
				}
				return
			}
			if terminal.Prepared == nil {
				t.Fatal("completed stream has no prepared sample")
			}
			if err := terminal.Prepared.Finalize(); err != nil {
				t.Fatalf("finalize prepared sample: %v", err)
			}
			if history := conversation.historySnapshot(); len(history) != 1 || len(history[0].Outputs) != 1 || history[0].Outputs[0].ID != "msg-1" {
				t.Fatalf("committed history: %#v", history)
			}
		})
	}
}

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
			var rawRequests [][]byte
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
				rawRequests = append(rawRequests, append([]byte(nil), body...))
				turn := len(requests)
				mu.Unlock()

				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp"+string(rune('0'+turn))+"\"}}\n\n")
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
			firstCompiled, err := compileResponsesRequest("gpt-test", nil, NewUserItem("first"))
			if err != nil {
				t.Fatalf("compile first expected request: %v", err)
			}
			secondCompiled, err := compileResponsesRequest("gpt-test", []nativeTurn{{
				User: NewUserItem("first"),
				Outputs: []NativeItem{{
					Type: "message", ID: "msg-1", Role: "assistant",
					Content: []ContentPart{{Type: "output_text", Text: "answer"}},
				}},
			}}, NewUserItem("second"))
			if err != nil {
				t.Fatalf("compile second expected request: %v", err)
			}
			if !bytes.Equal(rawRequests[0], firstCompiled.Bytes()) || !bytes.Equal(rawRequests[1], secondCompiled.Bytes()) {
				t.Fatalf("HTTP body differs from compiler output:\nfirst=%s\nsecond=%s", rawRequests[0], rawRequests[1])
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
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-live\"}}\n\n")
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
