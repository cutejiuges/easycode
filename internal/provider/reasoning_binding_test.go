package provider

import (
	"testing"

	"easycode/internal/domain"
)

func TestPreparedReasoningBindingsAreIndependentAndSingleUse(t *testing.T) {
	bindings := []domain.ReasoningBinding{
		{Block: 0, Item: 2, Source: domain.ReasoningOpenAISummary, Availability: domain.ReasoningReadable},
		{Block: 1, Item: 0, Source: domain.ReasoningOpenAISummary, Availability: domain.ReasoningUnavailable},
	}
	presentation, err := domain.NewSamplePresentation(3, bindings)
	if err != nil {
		t.Fatal(err)
	}
	bindings[0].Item = 99
	envelope, err := NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	sample, err := NewPreparedSample(envelope, testSampleUsage(t), syntheticRequestReceipt(domain.RequestSample), presentation, func() {}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	view, err := sample.Presentation()
	if err != nil || view.ItemCount() != 3 || view.Bindings()[0].Item != 2 {
		t.Fatal("incorrect prepared presentation", err)
	}
	copy := view.Bindings()
	copy[0].Block = 55
	again, _ := sample.Presentation()
	if again.Bindings()[0].Block != 0 {
		t.Fatal("binding collection was shared")
	}
	if _, err := NewCompletedStreamEvent(sample); err != nil {
		t.Fatal(err)
	}
	if err := sample.Discard(); err != nil {
		t.Fatal(err)
	}
	if _, err := sample.Presentation(); err == nil {
		t.Fatal("resolved presentation remained consumable")
	}
}

func TestSampleReasoningBindingsRejectAmbiguousPositions(t *testing.T) {
	v := domain.ReasoningBinding{Block: 0, Item: 1, Source: domain.ReasoningOpenAISummary, Availability: domain.ReasoningReadable}
	for _, bindings := range [][]domain.ReasoningBinding{
		{v, v},
		{v, {Block: 1, Item: 1, Source: v.Source, Availability: v.Availability}},
		{{Block: 1, Item: 0, Source: v.Source, Availability: v.Availability}},
		{{Block: 0, Item: 2, Source: v.Source, Availability: v.Availability}},
		{{Block: 0, Item: 0, Source: "raw", Availability: v.Availability}},
		make([]domain.ReasoningBinding, domain.MaxReasoningNodes+1),
	} {
		if _, err := domain.NewSamplePresentation(2, bindings); err == nil {
			t.Fatalf("accepted ambiguous or oversized bindings: count=%d", len(bindings))
		}
	}
	if (domain.SamplePresentation{}).Validate() == nil {
		t.Fatal("accepted an unconstructed presentation")
	}
	if domain.EmptySamplePresentation(0).Validate() != nil {
		t.Fatal("rejected explicitly empty presentation")
	}
}

func TestPreparedReasoningSourceMustMatchNativeFamily(t *testing.T) {
	envelope, _ := NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", []byte(`{}`))
	presentation, _ := domain.NewSamplePresentation(1, []domain.ReasoningBinding{{Block: 0, Item: 0, Source: domain.ReasoningAnthropicThinking, Availability: domain.ReasoningUnavailable}})
	if _, err := NewPreparedSample(envelope, testSampleUsage(t), syntheticRequestReceipt(domain.RequestSample), presentation, func() {}, func() {}); err == nil {
		t.Fatal("cross-provider reasoning binding accepted")
	}
}
