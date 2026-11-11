package context

import (
	"bytes"
	"testing"

	"easycode/internal/domain"
)

func TestReasoningProfileDefaultBytesAndAttributedChange(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderAnthropic, domain.ProviderOpenAI} {
		profile, err := NewProviderProfile(family, "fixture-model")
		if err != nil {
			t.Fatal(err)
		}
		base, revision, err := profile.canonical()
		if err != nil {
			t.Fatal(err)
		}
		want := `{"family":"` + string(family) + `","model":"fixture-model","schema_revision":"provider-profile-v1"}`
		if string(base.Bytes()) != want || revision != providerProfileRevision {
			t.Fatal("default profile bytes/revision changed")
		}
		if family == domain.ProviderAnthropic {
			explicit, _ := domain.NewAnthropicInference(domain.ThinkingSettings{}, "", 4096)
			p, err := profile.WithInferenceSettings(explicit)
			if err != nil {
				t.Fatal(err)
			}
			value, rev, _ := p.canonical()
			if !bytes.Equal(value.Bytes(), base.Bytes()) || rev != revision {
				t.Fatal("equivalent default output changes profile")
			}
		}
	}
	input := mustPlanningInput(t, domain.SemanticHistoryView{Provider: domain.ProviderOpenAI}, estimatedFootprint(t, domain.ProviderOpenAI, 0, 0), "", DisabledBudget())
	baseline, err := NewPlanner().Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, effort := range []domain.OpenAIEffort{"low", "high"} {
		s, _ := domain.NewOpenAIInference(effort, "auto", 16384)
		input.profile, err = input.profile.WithInferenceSettings(s)
		if err != nil {
			t.Fatal(err)
		}
		changed, err := NewPlanner().Plan(input)
		if err != nil {
			t.Fatal(err)
		}
		a, b := baseline.CachePlan().Segments(), changed.CachePlan().Segments()
		if a[0].Fingerprint() == b[0].Fingerprint() || a[0].Revision() == b[0].Revision() {
			t.Fatal("policy not attributed to profile")
		}
		for i := 1; i < len(a); i++ {
			if a[i].Fingerprint() != b[i].Fingerprint() {
				t.Fatal("policy polluted unrelated segment", i)
			}
		}
		left, _ := baseline.CachePlan().StablePrefixFingerprint()
		right, _ := changed.CachePlan().StablePrefixFingerprint()
		if left == right {
			t.Fatal("stable prefix ignored policy")
		}
		baseline = changed
	}
	profile, _ := NewProviderProfile(domain.ProviderAnthropic, "fixture-model")
	var previous []byte
	for _, display := range []domain.ThinkingDisplay{"", domain.DisplaySummarized, domain.DisplayOmitted} {
		thinking, _ := domain.NewThinkingSettings(domain.ThinkingAdaptive, 0, display)
		s, _ := domain.NewAnthropicInference(thinking, "high", 0)
		p, _ := profile.WithInferenceSettings(s)
		identity, rev, err := p.canonical()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(identity.Bytes(), previous) || rev == providerProfileRevision {
			t.Fatal("display omitted from stable identity")
		}
		previous = identity.Bytes()
	}
}
