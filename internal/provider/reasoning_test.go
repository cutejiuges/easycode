package provider

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

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
