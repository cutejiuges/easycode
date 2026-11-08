package protocol

import (
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestReasoningEventsStrictLifecycle(t *testing.T) {
	source := domain.ReasoningOpenAISummary
	start, err := NewReasoningStarted(0, source)
	if err != nil {
		t.Fatal(err)
	}
	delta, _ := NewReasoningDelta(0, source, 0, "中文")
	completion := ReasoningCompletion{Availability: domain.ReasoningReadable, TotalBytes: 6, Text: "中文"}
	end, _ := NewReasoningCompleted(0, source, completion)
	var state ReasoningStreamState
	for i, event := range []Event{start, delta, end} {
		if err := event.Validate(); err != nil {
			t.Fatal(err)
		}
		bound, err := BindReasoningSample(event, 3)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := DecodeAssistantReasoning(bound)
		if err != nil || payload.SampleIndex != 3 || state.Accept(payload) != nil {
			t.Fatal("failed reasoning event", err)
		}
		if i < 2 && state.Complete() == nil {
			t.Fatal("open block accepted as a successful sample")
		}
		for _, invalid := range []string{
			strings.Replace(string(bound.Payload), `"stage":`, `"unknown":true,"stage":`, 1),
			strings.Replace(string(bound.Payload), `"block":0`, `"block":null`, 1),
			strings.Replace(string(bound.Payload), `"source":`, `"source":"raw","source":`, 1),
		} {
			bad := bound
			bad.Payload = []byte(invalid)
			if bad.Validate() == nil {
				t.Fatalf("accepted invalid reasoning: %s", invalid)
			}
		}
	}
	if err := state.Complete(); err != nil {
		t.Fatal(err)
	}
	for _, event := range []Event{start, delta, end} {
		bound, _ := BindReasoningSample(event, 3)
		p, _ := DecodeAssistantReasoning(bound)
		if state.Accept(p) == nil {
			t.Fatal("closed block accepted another event")
		}
	}
	state.Reset()
	p, _ := DecodeAssistantReasoning(start)
	if state.Accept(p) != nil {
		t.Fatal("failed sample did not release its state")
	}
	p, _ = DecodeAssistantReasoning(delta)
	p.SampleIndex = 9
	if state.Accept(p) == nil {
		t.Fatal("cross-sample delta accepted")
	}
}

func TestReasoningRangesAndClosedStageFields(t *testing.T) {
	source := domain.ReasoningAnthropicThinking
	if _, err := NewReasoningDelta(0, source, 0, strings.Repeat("x", MaxReasoningDeltaBytes+1)); err == nil {
		t.Fatal("accepted large delta")
	}
	if _, err := NewReasoningStarted(domain.MaxReasoningNodes, source); err == nil {
		t.Fatal("accepted large block ordinal")
	}
	for _, c := range []ReasoningCompletion{
		{Availability: domain.ReasoningUnavailable, TotalBytes: 1, Text: "x"},
		{Availability: domain.ReasoningReadable},
		{Availability: domain.ReasoningReadable, TotalBytes: 2, Text: "x"},
		{Availability: domain.ReasoningReadable, Offset: 2, TotalBytes: 1},
		{Availability: domain.ReasoningReadable, TotalBytes: 1, Text: "\xff"},
	} {
		if _, err := NewReasoningCompleted(0, source, c); err == nil {
			t.Fatalf("accepted invalid completion: %#v", c)
		}
	}
	event, err := NewReasoningCompleted(0, source, ReasoningCompletion{Availability: domain.ReasoningUnavailable})
	if err != nil || event.Validate() != nil {
		t.Fatal("unavailable completion rejected", err)
	}
	for _, data := range []string{
		`{"sample_index":0,"block":0,"source":"anthropic_thinking","stage":"started","delta":{"offset":0,"text":"x"}}`,
		`{"sample_index":0,"block":0,"source":"anthropic_thinking","stage":"delta","delta":{"offset":0,"text":"x","private":"y"}}`,
		`{"sample_index":0,"block":0,"source":"anthropic_thinking","stage":"started","completion":null}`,
	} {
		event.Payload = []byte(data)
		if event.Validate() == nil {
			t.Fatal("accepted cross-stage or private fields")
		}
	}
}

func TestReasoningFailedOffsetsDoNotMutateState(t *testing.T) {
	start, _ := NewReasoningStarted(0, domain.ReasoningOpenAISummary)
	delta, _ := NewReasoningDelta(0, domain.ReasoningOpenAISummary, 1, "bad")
	var s ReasoningStreamState
	p, _ := DecodeAssistantReasoning(start)
	if s.Accept(p) != nil {
		t.Fatal("start failed")
	}
	p, _ = DecodeAssistantReasoning(delta)
	if s.Accept(p) == nil {
		t.Fatal("gap accepted")
	}
	p.Delta.Offset = 0
	if s.Accept(p) != nil {
		t.Fatal("invalid update corrupted state")
	}
}
