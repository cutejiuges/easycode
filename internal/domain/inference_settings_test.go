package domain

import (
	"bytes"
	"testing"
)

func TestInferenceClosedBranchesAndIsolation(t *testing.T) {
	if (InferenceSettings{}).Validate() == nil {
		t.Fatal("uninitialized settings accepted")
	}
	for _, tc := range []struct {
		mode    ThinkingMode
		budget  uint64
		display ThinkingDisplay
		valid   bool
	}{
		{ThinkingOmit, 0, "", true}, {ThinkingDisabled, 0, "", true}, {ThinkingAdaptive, 0, "", true},
		{ThinkingAdaptive, 0, DisplaySummarized, true}, {ThinkingEnabled, 1024, DisplayOmitted, true},
		{ThinkingDisabled, 0, DisplayOmitted, false}, {ThinkingAdaptive, 1024, "", false},
		{ThinkingEnabled, 1023, "", false}, {"updates", 0, "", false}, {ThinkingAdaptive, 0, "updates", false},
	} {
		got, err := NewThinkingSettings(tc.mode, tc.budget, tc.display)
		if (err == nil) != tc.valid {
			t.Fatalf("%+v: %v", tc, err)
		}
		if err == nil && (got.Mode() != tc.mode || got.BudgetTokens() != tc.budget || got.Display() != tc.display) {
			t.Fatal("selection changed")
		}
	}
	thinking, _ := NewThinkingSettings(ThinkingEnabled, 2048, DisplayOmitted)
	s, err := NewAnthropicInference(thinking, "high", 8192)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := s.RequestPolicyIdentity()
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), identity...)
	identity[0] = '['
	next, _ := s.RequestPolicyIdentity()
	if !bytes.Equal(original, next) {
		t.Fatal("identity aliases mutable buffer")
	}
	copy := s.Thinking()
	copy.mode = ThinkingDisabled
	if s.Thinking() != thinking || s.MaxOutputTokens() != 8192 {
		t.Fatal("getter mutated original")
	}
	if _, err := ResolveInferenceSettings(ProviderOpenAI, s); err == nil {
		t.Fatal("foreign family accepted")
	}
	for _, budget := range []uint64{1024, 4095, 4096} {
		thinking, _ := NewThinkingSettings(ThinkingEnabled, budget, "")
		_, err := NewAnthropicInference(thinking, "", 0)
		if (err == nil) != (budget < 4096) {
			t.Fatalf("budget %d: %v", budget, err)
		}
	}
}

func TestInferenceEffortEnumsAndDefaultIdentity(t *testing.T) {
	for _, v := range []AnthropicEffort{"", "low", "medium", "high", "xhigh", "max"} {
		if _, err := NewAnthropicInference(ThinkingSettings{}, v, 0); err != nil {
			t.Fatal(v, err)
		}
	}
	for _, v := range []OpenAIEffort{"", "none", "minimal", "low", "medium", "high", "xhigh", "max"} {
		for _, summary := range []ReasoningSummary{"", "auto", "concise", "detailed"} {
			if _, err := NewOpenAIInference(v, summary, 65536); err != nil {
				t.Fatal(v, summary, err)
			}
		}
	}
	if _, err := NewOpenAIInference("High", "", 0); err == nil {
		t.Fatal("effort alias accepted")
	}
	if _, err := NewOpenAIInference("", "raw", 0); err == nil {
		t.Fatal("unsupported summary accepted")
	}
	if _, err := NewOpenAIInference("", "", 65537); err == nil {
		t.Fatal("oversized output accepted")
	}
	disabled, _ := NewThinkingSettings(ThinkingDisabled, 0, "")
	for _, effort := range []AnthropicEffort{"xhigh", "max"} {
		if _, err := NewAnthropicInference(disabled, effort, 0); err == nil {
			t.Fatal("incompatible effort accepted")
		}
	}
	for _, output := range []uint64{0, 4096} {
		s, err := NewAnthropicInference(ThinkingSettings{}, "", output)
		if err != nil {
			t.Fatal(err)
		}
		identity, err := s.RequestPolicyIdentity()
		if err != nil || len(identity) != 0 {
			t.Fatal("default identity changed", err)
		}
		_, explicit := s.ExplicitOutput()
		if explicit != (output != 0) {
			t.Fatal("explicit source lost")
		}
	}
}
