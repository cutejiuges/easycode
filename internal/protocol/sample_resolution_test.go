package protocol

import (
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func TestCommitOriginsAndFailuresUseOneStrictContract(t *testing.T) {
	binding := domain.ReasoningBinding{Block: 0, Item: 2, Source: domain.ReasoningOpenAISummary, Availability: domain.ReasoningReadable}
	presentation, _ := domain.NewSamplePresentation(3, []domain.ReasoningBinding{binding})
	sample, err := NewSampleTranscriptCommitted(8, 4, presentation)
	if err != nil {
		t.Fatal(err)
	}
	p, err := DecodeTranscriptCommitted(sample)
	if err != nil || p.Origin != TranscriptCommitSample || p.Sample.SampleIndex != 4 || p.Sample.Bindings[0].Item != 2 {
		t.Fatal("sample binding lost", err)
	}
	input, err := NewInputTranscriptCommitted(9)
	if err != nil {
		t.Fatal(err)
	}
	ip, _ := DecodeTranscriptCommitted(input)
	if ip.Origin != TranscriptCommitInput || ip.Sample != nil {
		t.Fatal("input acquired a fake sample")
	}
	for _, invalid := range []string{
		`{"sequence":8}`,
		`{"origin":"input","sequence":8,"sample":null}`,
		strings.Replace(string(sample.Payload), `"origin":"sample"`, `"origin":"input"`, 1),
		strings.Replace(string(sample.Payload), `"bindings":[`, `"bindings":null,"unknown":[`, 1),
		strings.Replace(string(sample.Payload), `"item":2`, `"item":3`, 1),
		strings.Replace(string(sample.Payload), `"sequence":8`, `"sequence":8,"sequence":9`, 1),
	} {
		bad := sample
		bad.Payload = []byte(invalid)
		if _, err := DecodeTranscriptCommitted(bad); err == nil {
			t.Fatalf("invalid current commit accepted: %s", invalid)
		}
	}
	durable, err := NewDurableSampleResolution(4, 8, presentation)
	if err != nil {
		t.Fatal(err)
	}
	for _, resolution := range []SampleResolution{NewUncommittedSampleResolution(4), NewUncertainSampleResolution(4), durable} {
		failed, err := NewTurnSampleFailed("turn_failed", "safe failure", false, resolution)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeTurnFailed(failed)
		if err != nil || decoded.Resolution == nil || decoded.Resolution.Disposition != resolution.Disposition {
			t.Fatal("failure lost sample resolution", err)
		}
		bad := failed
		bad.Payload = []byte(strings.Replace(string(failed.Payload), `"disposition":`, `"private":true,"disposition":`, 1))
		if _, err := DecodeTurnFailed(bad); err == nil {
			t.Fatal("failure accepted unknown nested fields")
		}
	}
	durable.SampleIndex = 1
	if durable.Validate() == nil {
		t.Fatal("durable resolution accepted a different sample")
	}
	uncertain := NewUncertainSampleResolution(4)
	uncertain.Committed = &p
	if uncertain.Validate() == nil {
		t.Fatal("uncertain result acquired a committed location")
	}
}

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

func TestReasoningFailurePayloadPreflightsEscaping(t *testing.T) {
	resolution := NewUncertainSampleResolution(0)
	if _, err := NewTurnSampleFailed("session_write_failed", strings.Repeat("\x00", 100000), false, resolution); err == nil {
		t.Fatal("escaped payload over budget accepted")
	}
	for _, text := range []string{"safe\nquoted\"\\", "中文🙂<>&\u2028\u2029", "\x01\b\f\t"} {
		encoded, err := codec.MarshalStable(text)
		if err != nil {
			t.Fatal(err)
		}
		if jsonStringContentBytes(text) != len(encoded)-2 {
			t.Fatalf("escaping budget drift: %q / %s / %d", text, encoded, jsonStringContentBytes(text))
		}
	}
}
