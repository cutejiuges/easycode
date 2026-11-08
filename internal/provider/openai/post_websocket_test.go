package openai

import (
	"bytes"
	"context"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/tool"
	"fmt"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPostFeedbackWebSocketDurableSuffixDiscardAndRestoreReset(t *testing.T) {
	requests := make(chan []byte, 8)
	var count, connections atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		connections.Add(1)
		for {
			_, body, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			n := count.Add(1)
			requests <- body
			events := []string{fmt.Sprintf(`{"type":"response.created","response":{"id":"post-resp-%d"}}`, n)}
			if n == 1 {
				events = append(events,
					`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"post-reason","encrypted_content":"opaque-post"}}`,
					`{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"post-call","call_id":"call-post","name":"exec_command"}}`,
					`{"type":"response.function_call_arguments.done","output_index":1,"item_id":"post-call","arguments":"{\"command\":\"exit 7\",\"timeout_ms\":1000}"}`,
					`{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"post-call","call_id":"call-post","name":"exec_command","arguments":"{\"command\":\"exit 7\",\"timeout_ms\":1000}"}}`)

			} else {
				events = append(events, `{"type":"response.output_item.done","item":{"type":"message","id":"post-message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"post answer"}]}}`)
			}
			events = append(events, fmt.Sprintf(`{"type":"response.completed","response":{"id":"post-resp-%d"}}`, n))
			for _, event := range events {
				if err := conn.Write(r.Context(), websocket.MessageText, []byte(event)); err != nil {
					return
				}
			}
		}
	}))
	defer server.Close()
	policy, _ := domain.NewCachePolicy(domain.AnthropicCacheDisabled, domain.OpenAICacheThread, domain.OpenAIWebSocketAuto)
	instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("post-ws-fixture"), Model: "fixture-model", CachePolicy: policy})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	conversation := testConversation(t, instance)
	defer conversation.closeWebsocket()
	input := testOpenAITurnInput(t, conversation, "run checked command")
	stream, err := conversation.Stream(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	first := readOpenAITerminal(t, conversation, stream)
	if first.Kind() != provider.StreamEventCompleted {
		t.Fatal(first.Error())
	}
	<-requests
	if conversation.wsResponseID != "" {
		t.Fatal("response baseline advanced before sample finalize")
	}
	if err := first.PreparedSample().Finalize(); err != nil {
		t.Fatal(err)
	}
	result, _ := postProviderFixture(t, domain.HookExec)
	output := postProviderOutput(t, result)
	before := len(conversation.history.snapshot())
	prepared, err := conversation.PrepareToolOutputs([]tool.ModelOutput{output})
	if err != nil {
		t.Fatal(err)
	}
	if len(conversation.history.snapshot()) != before {
		t.Fatal("tool output advanced history before durable finalize")
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	sampling, err := provider.NewSamplingInput().WithToolCatalog(testOpenAIToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	stream, err = conversation.Stream(context.Background(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	second := readOpenAITerminal(t, conversation, stream)
	if second.Kind() != provider.StreamEventCompleted {
		t.Fatal(second.Error())
	}
	var suffix websocketCreate
	if err := codec.Unmarshal(<-requests, &suffix); err != nil {
		t.Fatal(err)
	}
	if suffix.PreviousResponseID != "post-resp-1" || len(suffix.Input) != 1 || suffix.Input[0].Type != "function_call_output" || !strings.Contains(suffix.Input[0].Output, "Read-only check:") {
		t.Fatal("post feedback not sent as sole durable suffix")
	}
	if err := second.PreparedSample().Discard(); err != nil {
		t.Fatal(err)
	}
	stream, err = conversation.Stream(context.Background(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	third := readOpenAITerminal(t, conversation, stream)
	if third.Kind() != provider.StreamEventCompleted {
		t.Fatal(third.Error())
	}
	wire := <-requests
	var reset websocketCreate
	_ = codec.Unmarshal(wire, &reset)
	if reset.PreviousResponseID != "" || connections.Load() != 2 || !bytes.Contains(wire, []byte("opaque-post")) || !bytes.Contains(wire, []byte("Read-only check:")) {
		t.Fatal("discard reset lost frozen feedback or opaque native order")
	}
	if err := third.PreparedSample().Finalize(); err != nil {
		t.Fatal(err)
	}
	var envelopes []provider.NativeCommitEnvelope
	for _, entry := range conversation.history.snapshot() {
		e, err := encodeNativeCommit(entry)
		if err != nil {
			t.Fatal(err)
		}
		envelopes = append(envelopes, e)
	}
	records := testRestoreRecords(t, envelopes, []tool.ModelOutput{output})
	restoredValue, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", records)
	if err != nil {
		t.Fatal(err)
	}
	restored := restoredValue.(*Conversation)
	defer restored.closeWebsocket()
	next := testOpenAITurnInput(t, restored, "continue restored checked history")
	expected, err := restored.compileSamplingRequest(restored.history.snapshot(), next)
	if err != nil {
		t.Fatal(err)
	}
	stream, err = restored.Stream(context.Background(), next)
	if err != nil {
		t.Fatal(err)
	}
	fourth := readOpenAITerminal(t, restored, stream)
	if fourth.Kind() != provider.StreamEventCompleted {
		t.Fatal(fourth.Error())
	}
	actual := <-requests
	full, err := websocketBody(expected, "", 0)
	if err != nil || !bytes.Equal(actual, full.Bytes()) || connections.Load() != 3 {
		t.Fatal("restored post history reused an unsafe incremental baseline", err)
	}
	_ = fourth.PreparedSample().Discard()
}
