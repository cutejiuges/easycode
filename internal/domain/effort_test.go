package domain

import (
	"errors"
	"testing"
)

func TestEffortChoicesPreserveNativeSettings(t *testing.T) {
	thinking, _ := NewThinkingSettings(ThinkingEnabled, 2048, DisplayOmitted)
	anthropic, _ := NewAnthropicInference(thinking, "high", 0)
	openai, _ := NewOpenAIInference("high", "detailed", 8192)
	for _, startup := range []InferenceSettings{anthropic, openai} {
		levels, err := EffortLevels(startup.Family())
		if err != nil {
			t.Fatal(err)
		}
		for _, level := range levels {
			choice, err := NewExplicitEffortChoice(level)
			if err != nil {
				t.Fatal(err)
			}
			next, err := choice.Apply(startup)
			if err != nil {
				t.Fatal(err)
			}
			if next.Effort() != level || startup.Effort() != "high" || next.Thinking() != startup.Thinking() || next.Summary() != startup.Summary() || next.output != startup.output {
				t.Fatalf("changed unrelated settings: %s", level)
			}
		}
		auto, err := NewAutomaticEffortChoice().Apply(startup)
		if err != nil || auto.Effort() != "" || auto.output != startup.output {
			t.Fatal("invalid automatic choice", err)
		}
		reset, err := NewResetEffortChoice().Apply(startup)
		if err != nil || reset != startup {
			t.Fatal("reset did not restore startup", err)
		}
	}
	if _, explicit := anthropic.ExplicitOutput(); explicit {
		t.Fatal("implicit cap became explicit")
	}
	none, _ := NewExplicitEffortChoice("none")
	sent, _ := none.Apply(openai)
	omitted, _ := NewAutomaticEffortChoice().Apply(openai)
	a, _ := sent.RequestPolicyIdentity()
	b, _ := omitted.RequestPolicyIdentity()
	if string(a) == string(b) {
		t.Fatal("none and omission have identical request identities")
	}
}

func TestEffortRejectsInvalidAndConflictingChoices(t *testing.T) {
	disabled, _ := NewThinkingSettings(ThinkingDisabled, 0, "")
	startup, _ := NewAnthropicInference(disabled, "high", 8192)
	for _, test := range []struct {
		level EffortLevel
		kind  EffortErrorKind
	}{{"none", EffortInvalid}, {"minimal", EffortInvalid}, {"max", EffortConflict}, {"xhigh", EffortConflict}} {
		choice, _ := NewExplicitEffortChoice(test.level)
		_, err := choice.Apply(startup)
		var failure *EffortError
		if !errors.As(err, &failure) || failure.Kind() != test.kind {
			t.Fatalf("%s: %v", test.level, err)
		}
	}
	for _, level := range []EffortLevel{"", "HIGH", "auto", "unknown"} {
		if _, err := NewExplicitEffortChoice(level); err == nil {
			t.Fatalf("accepted %q", level)
		}
	}
	if (EffortChoice{}).Validate() == nil {
		t.Fatal("accepted missing choice")
	}
}
