package tool

import (
	"strings"
	"testing"
)

func TestReadExecutionValidatesCompleteObservationBinding(t *testing.T) {
	id, _ := GenerateInvocationID()
	input, _ := NewReadInput("a", 1, 2000)
	call, _ := NewReadReadyCall("read-call", input)
	v, _ := NewReadInvocation(id, call)
	result := RenderReadSuccess(v, "a", []string{"data"}, 1)
	evidence, _ := NewFileBaseEvidence(NewContentDigest([]byte("data")), 4, 0o600, 1, 2)
	o, err := NewCompleteReadObservation(result, evidence)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewReadExecution(result, o); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"path", "identity", "offset", "eof", "long", "budget", "error"} {
		t.Run(change, func(t *testing.T) {
			bad := result
			switch change {
			case "path":
				bad.readMetadata.relativePath = "b"
			case "identity":
				bad.invocationID, _ = GenerateInvocationID()
			case "offset":
				bad.readMetadata.requestedOffset = 2
			case "eof":
				bad.readMetadata.reachedEOF = false
			case "long":
				bad.readMetadata.longLineTruncated = true
			case "budget":
				bad.readMetadata.outputTruncated = true
			case "error":
				bad.status = ResultError
			}
			if _, err := NewReadExecution(bad, o); err == nil {
				t.Fatal("接受了不匹配证据")
			}
		})
	}
	o = UnavailableReadObservation()
	o.evidence = evidence
	if o.Validate() == nil {
		t.Fatal("不可用观察包含证据")
	}
}

func TestReadBudgetRejectionDropsCompleteEvidence(t *testing.T) {
	id, _ := GenerateInvocationID()
	input, _ := NewReadInput("a", 1, 2000)
	call, _ := NewReadReadyCall("read-call", input)
	v, _ := NewReadInvocation(id, call)
	text := strings.Repeat("x", 1500)
	result := RenderReadSuccess(v, "a", []string{text}, 1)
	evidence, _ := NewFileBaseEvidence(NewContentDigest([]byte(text)), int64(len(text)), 0600, 1, 2)
	observation, err := NewCompleteReadObservation(result, evidence)
	if err != nil {
		t.Fatal(err)
	}
	execution, _ := NewReadExecution(result, observation)
	final, err := execution.Finalize(SafetyResultBytes)
	if err != nil {
		t.Fatal(err)
	}
	if final.Result().Code() != "result_budget_exceeded" || final.Observation().Kind() != ReadObservationUnavailable || final.Result().ReadMetadata().ReachedEOF() || !strings.Contains(final.Result().Preview().Text(), "smaller limit") {
		t.Fatal("budget rejection retained evidence or lost retry hint")
	}
	if final.Result().Budget().ActualBytes() != len(final.Result().Preview().Text()) || final.Result().Budget().Reason() != BudgetRejected {
		t.Fatal("incorrect rejection accounting")
	}
	preserved, err := execution.Finalize(MaxModelPreviewBytes)
	if err != nil || preserved.Observation() != observation || preserved.Result().Preview() != result.Preview() {
		t.Fatal("sufficient budget changed Read")
	}
}
