package protocol

import (
	"testing"

	"easycode/internal/domain"
)

func TestReasoningFailureClosesOpenBlocksAndCommitChecksMapping(t *testing.T) {
	var state ReasoningStreamState
	start, _ := NewReasoningStarted(0, domain.ReasoningOpenAISummary)
	p, _ := DecodeAssistantReasoning(start)
	p.SampleIndex = 4
	if state.Accept(p) != nil {
		t.Fatal("start rejected")
	}
	if state.Resolve(NewUncommittedSampleResolution(5)) == nil {
		t.Fatal("wrong sample resolution accepted")
	}
	if state.Resolve(NewUncertainSampleResolution(4)) != nil || state.Complete() != nil {
		t.Fatal("open blocks did not close on uncertain failure")
	}
	if state.Accept(p) != nil {
		t.Fatal("next sample state not reusable")
	}
	presentation, _ := domain.NewSamplePresentation(1, []domain.ReasoningBinding{{Block: 0, Item: 0, Source: p.Source, Availability: domain.ReasoningUnavailable}})
	commit, _ := NewSampleTranscriptCommitted(5, 4, presentation)
	cp, _ := DecodeTranscriptCommitted(commit)
	if state.Commit(*cp.Sample) == nil {
		t.Fatal("open block committed without a completion")
	}
	end, _ := NewReasoningCompleted(0, p.Source, ReasoningCompletion{Availability: domain.ReasoningUnavailable})
	p, _ = DecodeAssistantReasoning(end)
	p.SampleIndex = 4
	if state.Accept(p) != nil || state.Commit(*cp.Sample) != nil {
		t.Fatal("valid committed binding rejected")
	}
}
