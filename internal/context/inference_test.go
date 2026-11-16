package context

import (
	"testing"

	"easycode/internal/domain"
)

func TestEffortBudgetAndProfileIdentity(t *testing.T) {
	budget, _ := NewBudget(32768, 8192, 1024)
	startup, _ := domain.NewOpenAIInference("", "", 0)
	if err := budget.ValidateInference(startup); err != nil {
		t.Fatal(err)
	}
	choice, _ := domain.NewExplicitEffortChoice("high")
	settings, _ := choice.Apply(startup)
	if budget.ValidateInference(settings) == nil {
		t.Fatal("accepted reasoning without explicit cap")
	}
	allowed, err := AllowedEffortLevels(startup, budget)
	if err != nil || len(allowed) != 0 {
		t.Fatal("advertised invalid levels", allowed, err)
	}
	startup, _ = domain.NewOpenAIInference("high", "auto", 8192)
	profile, _ := NewProviderProfile(domain.ProviderOpenAI, "fixture-model")
	profile, _ = profile.WithInferenceSettings(startup)
	before, rev, _ := profile.canonical()
	settings, _ = choice.Apply(startup)
	same, _ := profile.WithInferenceSettings(settings)
	after, sameRev, _ := same.canonical()
	if string(before.Bytes()) != string(after.Bytes()) || rev != sameRev {
		t.Fatal("same settings changed profile")
	}
	low, _ := domain.NewExplicitEffortChoice("low")
	settings, _ = low.Apply(startup)
	different, _ := profile.WithInferenceSettings(settings)
	changed, changedRev, _ := different.canonical()
	if string(changed.Bytes()) == string(before.Bytes()) || changedRev == rev {
		t.Fatal("changed effort did not change profile")
	}
	disabled, _ := domain.NewThinkingSettings(domain.ThinkingDisabled, 0, "")
	anthropic, _ := domain.NewAnthropicInference(disabled, "high", 8192)
	allowed, err = AllowedEffortLevels(anthropic, budget)
	if err != nil || len(allowed) != 3 || allowed[2] != "high" {
		t.Fatal("invalid filtered levels", allowed, err)
	}
	tooSmall, _ := NewBudget(32768, 4096, 1024)
	if tooSmall.ValidateInference(startup) == nil {
		t.Fatal("accepted insufficient reserve")
	}
}
