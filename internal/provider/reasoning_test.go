package provider

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func TestReasoningPublisherLazyUTF8AndCompletion(t *testing.T) {
	text := strings.Repeat("中文🙂\x00", 4000)
	body, err := domain.NewReasoningBody(domain.ReasoningOpenAISummary, []string{text})
	if err != nil {
		t.Fatal(err)
	}
	start, _ := NewReasoningStartDraft(0, body.Source())
	delta, _ := NewReasoningBodyTextDraft(0, body, 0)
	complete, _ := NewReasoningCompletionDraft(0, body)
	output := make(chan StreamEvent, 16)
	first, _ := NewReasoningDrafts(start, delta)
	if err := PublishReasoning(context.Background(), output, first); err != nil {
		t.Fatal(err)
	}
	last, _ := NewReasoningDrafts(complete)
	if err := PublishReasoning(context.Background(), output, last); err != nil {
		t.Fatal(err)
	}
	close(output)
	var state protocol.ReasoningStreamState
	var assembled strings.Builder
	for event := range output {
		p, err := protocol.DecodeAssistantReasoning(event.Semantic())
		if err != nil {
			t.Fatal(err)
		}
		if err := state.Accept(p); err != nil {
			t.Fatal(err)
		}
		if len(event.Semantic().Payload) > protocol.MaxReasoningPayloadBytes {
			t.Fatal("escaped payload exceeded budget")
		}
		if p.Delta != nil {
			if !utf8.ValidString(p.Delta.Text) || len(p.Delta.Text) > 16<<10 {
				t.Fatal("invalid chunk")
			}
			assembled.WriteString(p.Delta.Text)
		}
		if p.Completion != nil && (p.Completion.Text != text || p.Completion.TotalBytes != uint64(len(text))) {
			t.Fatal("completion changed source")
		}
	}
	if assembled.String() != text || state.Complete() != nil {
		t.Fatal("source changed or block left open")
	}
}

func TestReasoningPublisherBackpressureCancellation(t *testing.T) {
	text := strings.Repeat("x", domain.MaxReasoningSourceBytes)
	delta, _ := NewReasoningTextDraft(0, domain.ReasoningOpenAISummary, 0, text)
	drafts, _ := NewReasoningDrafts(delta)
	output := make(chan StreamEvent)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- PublishReasoning(ctx, output, drafts) }()
	event := <-output
	p, _ := protocol.DecodeAssistantReasoning(event.Semantic())
	if len(p.Delta.Text) != protocol.MaxReasoningDeltaBytes {
		t.Fatal("first chunk invalid")
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatal(err)
	}
	select {
	case <-output:
		t.Fatal("published after owner completed")
	default:
	}
}

func TestReasoningBudgetChecksCumulativeAndReplacement(t *testing.T) {
	var b ReasoningBudget
	if err := b.ReplaceSource(0, domain.MaxReasoningSourceBytes-1); err != nil {
		t.Fatal(err)
	}
	if b.ReplaceSource(0, 2) == nil {
		t.Fatal("cumulative source accepted")
	}
	if err := b.ReplaceSource(domain.MaxReasoningSourceBytes-1, domain.MaxReasoningSourceBytes-1); err != nil {
		t.Fatal("same done double charged")
	}
	if err := b.ReplaceScratch(0, domain.MaxReasoningSourceBytes-1); err != nil {
		t.Fatal(err)
	}
	if b.CheckScratch(2) == nil || b.ReplaceScratch(0, 2) == nil {
		t.Fatal("cumulative scratch accepted")
	}
	var nodes ReasoningBudget
	for range domain.MaxReasoningNodes {
		if err := nodes.ReserveNode(0); err != nil {
			t.Fatal(err)
		}
	}
	if nodes.ReserveNode(0) == nil {
		t.Fatal("unbounded nodes")
	}
	if _, err := NewReasoningDrafts(ReasoningDraft{}, ReasoningDraft{}, ReasoningDraft{}); err == nil {
		t.Fatal("unbounded drafts")
	}
}

func TestReasoningEscapedCompletionAndInFlightCopies(t *testing.T) {
	body, err := domain.NewReasoningBody(domain.ReasoningAnthropicThinking, []string{strings.Repeat("\x00", protocol.MaxReasoningPreviewBytes)})
	if err != nil {
		t.Fatal(err)
	}
	complete, _ := NewReasoningCompletionDraft(0, body)
	drafts, _ := NewReasoningDrafts(complete)
	output := make(chan StreamEvent, 1)
	if err := PublishReasoning(context.Background(), output, drafts); err != nil {
		t.Fatal(err)
	}
	event := <-output
	wire := event.Semantic()
	if len(wire.Payload) > protocol.MaxReasoningPayloadBytes || len(wire.Payload) < 6*protocol.MaxReasoningPreviewBytes {
		t.Fatal("escaping was not bounded or counted")
	}
	payload, err := protocol.DecodeAssistantReasoning(wire)
	if err != nil || payload.Completion.Text != strings.Repeat("\x00", protocol.MaxReasoningPreviewBytes) {
		t.Fatal("escaped snapshot changed", err)
	}
	// 发布/绑定/控制投影/宿主访问最多同时持有八份有界 payload，转发适配器没有第三队列。
	if 8*protocol.MaxReasoningPayloadBytes > 4<<20 {
		t.Fatal("in-flight payload copy budget exceeded")
	}
}

func TestReasoningSemanticRejectsOversizeBeforeCopy(t *testing.T) {
	event, _ := protocol.NewReasoningStarted(0, domain.ReasoningOpenAISummary)
	event.Payload = []byte(strings.Repeat("x", protocol.MaxReasoningPayloadBytes+1))
	benchmark := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			if _, err := NewSemanticStreamEvent(event); err == nil {
				b.Fatal("oversize reasoning accepted")
			}
		}
	})
	if benchmark.AllocedBytesPerOp() > 1024 {
		t.Fatal("invalid semantic payload was copied before validation", benchmark.AllocedBytesPerOp())
	}
}
