package provider

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider/transport"
)

func TestReasoningPublisherBackpressureCancellation(t *testing.T) {
	timeout, stopTimeout := context.WithTimeout(t.Context(), 5*time.Second)
	defer stopTimeout()
	ctx, cancel := context.WithCancel(timeout)
	defer cancel()
	transportCtx, stopTransport := context.WithCancel(ctx)
	defer stopTransport()

	input := make(chan transport.SSEMessage, sampleProgressCapacity+2)
	transportDone := make(chan struct{})
	for range cap(input) {
		input <- transport.SSEMessage{Event: &transport.SSEEvent{}}
	}
	go func() {
		<-transportCtx.Done()
		close(transportDone)
		close(input)
	}()

	output := newSampleOutput()
	ops := &ConversationOperations{}
	ops.Begin()
	full, advanced, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var frames int
	var cleanupCompleted bool
	text := strings.Repeat("x", protocol.MaxReasoningDeltaBytes)
	stream := SampleStream{
		Messages: input,
		Reduce: func(transport.SSEEvent) (SampleReduction, error) {
			frames++
			switch frames {
			case sampleProgressCapacity + 1:
				close(full)
			case sampleProgressCapacity + 2:
				close(advanced)
			}
			delta, _ := NewReasoningTextDraft(0, domain.ReasoningOpenAISummary, uint64((frames-1)*len(text)), text)
			drafts, err := NewReasoningDrafts(delta)
			return SampleReduction{Reasoning: drafts}, err
		},
		Cleanup: func(success bool) {
			select {
			case <-transportDone:
				cleanupCompleted = !success && ops.Busy()
			default:
			}
		},
	}
	go func() {
		defer close(done)
		stream.run(ctx, stopTransport, ops, output, SampleStreamErrors{ProviderLabel: "fixture"})
	}()
	wait := func(ch <-chan struct{}, stage string) {
		t.Helper()
		select {
		case <-ch:
		case <-timeout.Done():
			t.Fatalf("stream did not reach %s", stage)
		}
	}
	wait(full, "publication capacity")
	checkDelta := func(event StreamEvent, index int) {
		t.Helper()
		p, err := protocol.DecodeAssistantReasoning(event.Semantic())
		if err != nil || p.Delta == nil || len(p.Delta.Text) != len(text) || p.Delta.Offset != uint64(index*len(text)) {
			t.Fatalf("unexpected reasoning chunk %d: %v", index, err)
		}
	}
	for event := range output.read {
		checkDelta(event, 0)
		break
	}
	wait(advanced, "progress after a consumer receive")
	cancel()
	wait(done, "cleanup without further reads")
	if !cleanupCompleted || ops.Busy() {
		t.Fatal("sample released ownership before cleanup or remained busy after cleanup")
	}
	count := 0
	for event := range output.read {
		count++
		if count <= sampleProgressCapacity {
			checkDelta(event, count)
		} else if event.Kind() != StreamEventCancelled || !errors.Is(event.Error(), context.Canceled) {
			t.Fatalf("unexpected terminal %s: %v", event.Kind(), event.Error())
		}
	}
	if count != sampleProgressCapacity+1 {
		t.Fatalf("remaining progress and terminal count = %d", count)
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
