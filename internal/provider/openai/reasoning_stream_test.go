package openai

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/provider/transport"
)

func reasoningFrames(t *testing.T, frames ...string) (*responsesStreamReducer, []protocol.AssistantReasoningPayload) {
	t.Helper()
	r := newResponsesStreamReducer()
	reduceOpenAIEvent(t, r, `{"type":"response.created","response":{"id":"r"}}`)
	var result []protocol.AssistantReasoningPayload
	for _, frame := range frames {
		reduced, err := r.reduce(transport.SSEEvent{Data: frame})
		if err != nil {
			t.Fatalf("frame %s: %v", frame, err)
		}
		output := make(chan provider.StreamEvent, 16)
		if err := provider.PublishReasoning(context.Background(), output, reduced.reasoning); err != nil {
			t.Fatal(err)
		}
		close(output)
		for event := range output {
			p, err := protocol.DecodeAssistantReasoning(event.Semantic())
			if err != nil {
				t.Fatal(err)
			}
			result = append(result, p)
		}
	}
	return r, result
}

func TestReasoningOutOfOrderPartsPublishOnlyOrderedPrefix(t *testing.T) {
	r, events := reasoningFrames(t,
		`{"type":"response.output_item.added","output_index":2,"item":{"type":"reasoning","id":"a","summary":[]}}`,
		`{"type":"response.reasoning_summary_text.done","output_index":2,"item_id":"a","summary_index":2,"text":"末🙂"}`,
		`{"type":"response.reasoning_summary_part.added","output_index":2,"item_id":"a","summary_index":0,"part":{"type":"summary_text","text":""}}`,
		`{"type":"response.reasoning_summary_text.delta","output_index":2,"item_id":"a","summary_index":0,"delta":"首 "}`,
		`{"type":"response.reasoning_summary_text.done","output_index":2,"item_id":"a","summary_index":0,"text":"首 "}`,
		`{"type":"response.reasoning_summary_part.done","output_index":2,"item_id":"a","summary_index":1,"part":{"type":"summary_text","text":""}}`,
		`{"type":"response.output_item.done","output_index":2,"item":{"type":"reasoning","id":"a","summary":[{"type":"summary_text","text":"首 "},{"type":"summary_text","text":""},{"type":"summary_text","text":"末🙂"}],"encrypted_content":"opaque-sentinel"}}`,
		`{"type":"response.reasoning_summary_text.done","output_index":2,"item_id":"a","summary_index":2,"text":"ignored-late"}`)
	var text strings.Builder
	var state protocol.ReasoningStreamState
	for _, p := range events {
		if err := state.Accept(p); err != nil {
			t.Fatal(err)
		}
		if p.Delta != nil {
			text.WriteString(p.Delta.Text)
		}
		if p.Completion != nil && p.Completion.Text != "首 \n\n末🙂" {
			t.Fatal("wrong final snapshot")
		}
	}
	if text.String() != "首 \n\n末🙂" {
		t.Fatal(text.String())
	}
	presentation, err := r.presentation()
	if err != nil {
		t.Fatal(err)
	}
	if len(presentation.Bindings()) != 1 || presentation.Bindings()[0].Item != 0 {
		t.Fatal("wrong native ordinal")
	}
	if r.items[0].EncryptedContent != "opaque-sentinel" {
		t.Fatal("opaque native data lost")
	}
	if _, err := r.reduce(transport.SSEEvent{Data: `{"type":"response.reasoning_summary_text.delta","output_index":2,"item_id":"a","summary_index":2,"delta":"late"}`}); err == nil {
		t.Fatal("closed delta accepted")
	}
}

func TestReasoningRejectsConflictsAndUnknownReferences(t *testing.T) {
	for _, frame := range []string{
		`{"type":"response.reasoning_summary_text.done","output_index":0,"item_id":"unknown","summary_index":0,"text":"x"}`,
		`{"type":"response.reasoning_summary_text.done","output_index":1,"item_id":"a","summary_index":0,"text":"x"}`,
		`{"type":"response.reasoning_summary_text.done","output_index":0,"item_id":"a","summary_index":4096,"text":"x"}`,
		`{"type":"response.reasoning_summary_text.delta","output_index":0,"item_id":"a","summary_index":1,"delta":"x"}`,
		`{"type":"response.reasoning_summary_text.done","output_index":0,"item_id":"a","summary_index":0,"text":"conflict"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"a","summary":[{"type":"summary_text","text":"conflict"}]}}`,
	} {
		r, _ := reasoningFrames(t, `{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"a","summary":[{"type":"summary_text","text":"initial"}]}}`)
		if _, err := r.reduce(transport.SSEEvent{Data: frame}); err == nil {
			t.Fatal("accepted invalid frame", frame)
		}
	}
}
