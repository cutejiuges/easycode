package openai

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
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

func TestReasoningDeliveryMatrix(t *testing.T) {
	tests := []struct {
		name   string
		frames []string
		want   string
	}{
		{"item-only", []string{`{"type":"response.output_item.done","item":{"type":"reasoning","summary":[{"type":"summary_text","text":" only "}],"content":[{"type":"reasoning_text","text":"raw-sentinel"}]}}`}, " only "},
		{"text-done", []string{`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"a"}}`, `{"type":"response.reasoning_summary_text.done","output_index":0,"item_id":"a","summary_index":0,"text":"snapshot"}`, `{"type":"response.reasoning_summary_text.done","output_index":0,"item_id":"a","summary_index":0,"text":"snapshot"}`, `{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"a","summary":[{"type":"summary_text","text":"snapshot"}]}}`}, "snapshot"},
		{"initial-and-final-fill", []string{`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"a","summary":[{"type":"summary_text","text":"initial"}]}}`, `{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"a","summary":[{"type":"summary_text","text":"initial"},{"type":"summary_text","text":"fill"}]}}`}, "initial\n\nfill"},
		{"part-done", []string{`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"a"}}`, `{"type":"response.reasoning_summary_part.done","output_index":0,"item_id":"a","summary_index":0,"part":{"type":"summary_text","text":"part"}}`, `{"type":"response.reasoning_summary_part.done","output_index":0,"item_id":"a","summary_index":0,"part":{"type":"summary_text","text":"part"}}`, `{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"a","summary":[{"type":"summary_text","text":"part"}]}}`}, "part"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, events := reasoningFrames(t, test.frames...)
			var state protocol.ReasoningStreamState
			starts, completed := 0, 0
			for _, p := range events {
				if err := state.Accept(p); err != nil {
					t.Fatal(err)
				}
				if p.Stage == protocol.ReasoningStarted {
					starts++
				}
				if p.Completion != nil {
					completed++
					if p.Completion.Text != test.want {
						t.Fatal(p.Completion.Text)
					}
				}
			}
			if starts != 1 || completed != 1 || state.Complete() != nil {
				t.Fatal("invalid lifecycle")
			}
		})
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

func TestReasoningPreflightCumulativeMetadataAndSourceBeforeMutation(t *testing.T) {
	r, _ := reasoningFrames(t, `{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"a","summary":[]}}`)
	for range domain.MaxReasoningNodes - 2 {
		if err := r.reasoningBudget.ReserveNode(0); err != nil {
			t.Fatal(err)
		}
	}
	frame := `{"type":"response.output_item.done","item":{"type":"reasoning","id":"another","summary":[{"type":"summary_text","text":"x"}]}}`
	if _, err := r.reduce(transport.SSEEvent{Data: frame}); err == nil {
		t.Fatal("complete item allocated beyond cumulative metadata")
	}
	if len(r.items) != 0 || r.reasoning["another"] != nil {
		t.Fatal("failed preflight changed staging")
	}
	r, _ = reasoningFrames(t, `{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"a","summary":[{"type":"summary_text","text":""}]}}`)
	// 多个小来源增量共享同一个 sample 预算，跨 item 不会重新置零。
	for range domain.MaxReasoningSourceBytes / (16 << 10) {
		if err := r.reasoningBudget.ReplaceSource(0, 16<<10); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.reduce(transport.SSEEvent{Data: `{"type":"response.reasoning_summary_text.delta","output_index":0,"item_id":"a","summary_index":0,"delta":"x"}`}); err == nil {
		t.Fatal("cumulative source bound bypassed")
	}
	if r.reasoning["a"].parts[0].text != "" {
		t.Fatal("source append preceded budget check")
	}
}

func TestReasoningItemOnlyKnownIndexLatePartDone(t *testing.T) {
	_, events := reasoningFrames(t,
		`{"type":"response.output_item.done","output_index":1,"item":{"type":"reasoning","id":"a","summary":[{"type":"summary_text","text":"once"}]}}`,
		`{"type":"response.reasoning_summary_part.done","output_index":1,"item_id":"a","summary_index":0,"part":{"type":"summary_text","text":"ignored late"}}`)
	if len(events) != 2 || events[0].Stage != protocol.ReasoningStarted || events[1].Stage != protocol.ReasoningCompleted {
		t.Fatal("item-only completion replayed", events)
	}
}

func TestReasoningUnknownInitialPartRemainsOpaqueAndStable(t *testing.T) {
	r, events := reasoningFrames(t,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"a","summary":[{"type":"future_part","text":"private-summary-sentinel"},{"type":"summary_text","text":"allowed"}]}}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"a","summary":[{"type":"future_part","text":"private-summary-sentinel"},{"type":"summary_text","text":"allowed"}]}}`)
	for _, event := range events {
		if event.Delta != nil && event.Delta.Text != "allowed" {
			t.Fatal("unknown part entered prefix")
		}
		if event.Completion != nil && event.Completion.Text != "allowed" {
			t.Fatal("unknown part entered completion")
		}
	}
	if r.items[0].ReasoningSummary[0].Text != "private-summary-sentinel" {
		t.Fatal("unknown native part lost")
	}
}
func TestReasoningItemOnlyCannotReuseAnOutputIndex(t *testing.T) {
	r, _ := reasoningFrames(t, `{"type":"response.output_item.done","output_index":1,"item":{"type":"reasoning","summary":[]}}`)
	if _, err := r.reduce(transport.SSEEvent{Data: `{"type":"response.output_item.done","output_index":1,"item":{"type":"reasoning","summary":[]}}`}); err == nil {
		t.Fatal("anonymous item reused known output index")
	}
}

func TestReasoningOpaquePartIdentityCountsAgainstMetadata(t *testing.T) {
	r := newResponsesStreamReducer()
	reduceOpenAIEvent(t, r, `{"type":"response.created","response":{"id":"r"}}`)
	frame := `{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"a","summary":[{"type":"` + strings.Repeat("k", provider.MaxReasoningMetadataBytes) + `","text":""}]}}`
	if _, err := r.reduce(transport.SSEEvent{Data: frame}); err == nil {
		t.Fatal("opaque part identity bypassed metadata budget")
	}
	if len(r.reasoning) != 0 || len(r.items) != 0 {
		t.Fatal("overbudget identity entered staging")
	}
}
