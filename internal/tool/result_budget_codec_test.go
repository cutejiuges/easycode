package tool

import (
	"strings"
	"testing"
)

func TestResultBudgetStrictCurrentCodec(t *testing.T) {
	for _, wire := range []string{
		`{}`, `{"allowance":1024,"reason":"preserved"}`, `{"allowance":1024,"actual_bytes":0}`, `{"allowance":0,"actual_bytes":0,"reason":"preserved"}`, `{"allowance":1024,"actual_bytes":1025,"reason":"preserved"}`, `{"allowance":1024,"actual_bytes":0,"reason":"unknown"}`, `{"allowance":1024,"actual_bytes":0,"reason":"preserved","extra":1}`, `{"allowance":1024,"allowance":2048,"actual_bytes":0,"reason":"preserved"}`, `{"allowance":1024,"actual_bytes":null,"reason":"preserved"}`,
	} {
		if _, err := DecodeResultBudgetMetadata([]byte(wire)); err == nil {
			t.Fatalf("accepted %s", wire)
		}
	}
	want, err := NewResultBudgetMetadata(1024, 37, BudgetShortened)
	if err != nil {
		t.Fatal(err)
	}
	data, err := want.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeResultBudgetMetadata(data)
	if err != nil || got != want {
		t.Fatalf("budget round trip: %v", err)
	}
}

func TestResultFinalizationPreservesModificationOutcome(t *testing.T) {
	id, _ := GenerateInvocationID()
	replacement, _ := NewPatchReplacement("old", "new", false)
	input, err := NewPatchInput(strings.Repeat("a", 3000), []PatchReplacement{replacement})
	if err != nil {
		t.Fatal(err)
	}
	call, err := NewPatchReadyCall("patch", WireStructured, input)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := NewPatchInvocation(id, call)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []ResultStatus{ResultDenied, ResultConflict, ResultCancelled, ResultOutcomeUncertain} {
		result := NewPatchErrorResult(invocation, status, "test_error", strings.Repeat("detail", 1000))
		final, err := FinalizeResult(result, SafetyResultBytes, BudgetPreserved)
		if err != nil {
			t.Fatal(err)
		}
		if final.Status() != status || final.Code() != result.Code() || final.PatchMetadata() != result.PatchMetadata() || len(final.Preview().Text()) > SafetyResultBytes {
			t.Fatal("finalization changed operation outcome")
		}
	}
}
