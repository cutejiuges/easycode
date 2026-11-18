package domain

import (
	"strings"
	"testing"
)

func TestReasoningLimitsBeforeCopyAndSmallPageAllocation(t *testing.T) {
	if _, err := NewReasoningBody(ReasoningOpenAISummary, make([]string, MaxReasoningNodes+1)); err == nil {
		t.Fatal("accepted excess parts")
	}
	if _, err := NewReasoningBody(ReasoningOpenAISummary, []string{strings.Repeat("x", MaxReasoningSourceBytes), "y"}); err == nil {
		t.Fatal("accepted excess joined bytes")
	}
	body, err := NewReasoningBody(ReasoningOpenAISummary, []string{strings.Repeat("x", 1<<20), "end"})
	if err != nil {
		t.Fatal(err)
	}
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			page, err := body.ReadRange(body.Len()-3, 3, false)
			if err != nil || page.Text != "end" {
				b.Fatal("incorrect final page", err)
			}
		}
	})
	if result.AllocedBytesPerOp() > 256 {
		t.Fatalf("small range copied a large body: %d bytes/op", result.AllocedBytesPerOp())
	}
}
