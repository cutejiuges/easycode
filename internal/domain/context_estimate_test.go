package domain

import "testing"

func TestTokenEstimatePreservesEstimatedAndUnknownStates(t *testing.T) {
	t.Parallel()
	estimated, err := NewEstimatedTokenEstimate("byte_heuristic_v1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if tokens, ok := estimated.Tokens(); !ok || tokens != 0 || estimated.State() != TokenEstimateEstimated {
		t.Fatalf("estimated zero = state %q tokens %d ok %t", estimated.State(), tokens, ok)
	}
	unknown, err := NewUnknownTokenEstimate("byte_heuristic_v1")
	if err != nil {
		t.Fatal(err)
	}
	if tokens, ok := unknown.Tokens(); ok || tokens != 0 || unknown.State() != TokenEstimateUnknown {
		t.Fatalf("unknown = state %q tokens %d ok %t", unknown.State(), tokens, ok)
	}
}

func TestTokenEstimateRejectsInvalidMethodAndState(t *testing.T) {
	t.Parallel()
	if _, err := NewEstimatedTokenEstimate(" ", 1); err == nil {
		t.Fatal("empty method unexpectedly accepted")
	}
	if err := (TokenEstimate{method: "v1", state: "future"}).Validate(); err == nil {
		t.Fatal("unknown state unexpectedly accepted")
	}
	if err := (TokenEstimate{method: "v1", state: TokenEstimateUnknown, tokens: 1}).Validate(); err == nil {
		t.Fatal("unknown estimate with value unexpectedly accepted")
	}
}

func TestNativeHistoryFootprintValidatesAndReturnsValues(t *testing.T) {
	t.Parallel()
	estimate, err := NewEstimatedTokenEstimate("byte_heuristic_v1", 42)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := NewNativeHistoryFootprint(ProviderOpenAI, 7, estimate)
	if err != nil {
		t.Fatal(err)
	}
	if footprint.Family() != ProviderOpenAI || footprint.Revision() != 7 || footprint.Estimate() != estimate {
		t.Fatalf("footprint = %#v", footprint)
	}
	if _, err := NewNativeHistoryFootprint("future", 0, estimate); err == nil {
		t.Fatal("invalid family unexpectedly accepted")
	}
	if _, err := NewNativeHistoryFootprint(ProviderOpenAI, 0, TokenEstimate{}); err == nil {
		t.Fatal("invalid estimate unexpectedly accepted")
	}
}
