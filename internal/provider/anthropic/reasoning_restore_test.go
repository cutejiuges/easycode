package anthropic

import (
	"os"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/provider/testsupport"
	"easycode/internal/secret"
)

func TestReasoningFixedNativeRestoreAndCacheBytesIgnoreReading(t *testing.T) {
	fixture, err := os.ReadFile("testdata/reasoning_restore.current.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []domain.AnthropicCacheMode{domain.AnthropicCacheDisabled, domain.AnthropicCache5m, domain.AnthropicCache1h} {
		policy, _ := domain.NewCachePolicy(mode, domain.OpenAICacheOmit, domain.OpenAIHTTPSSE)
		instance, err := New(Config{BaseURL: "https://example.invalid", Model: "claude-test", APIKey: secret.New("fixture-key"), CachePolicy: policy, Inference: reasoningRestoreSettings(t)})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer instance.Close()
			live := testConversation(t, instance)
			envelopes := testsupport.LoadNativeFixture(t, live, fixture)
			restoredValue, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", testRestoreRecords(t, envelopes))
			if err != nil {
				t.Fatal(err)
			}
			restored := restoredValue.(*Conversation)
			input, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			testsupport.CheckReasoningRestore(t, live, restored, input, 4)
		}()
	}
}

func reasoningRestoreSettings(t *testing.T) domain.InferenceSettings {
	t.Helper()
	thinking, err := domain.NewThinkingSettings(domain.ThinkingAdaptive, 0, domain.DisplayOmitted)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := domain.NewAnthropicInference(thinking, "high", 16384)
	if err != nil {
		t.Fatal(err)
	}
	return settings
}

func (c *Conversation) TestCacheSources(t *testing.T) contextplan.CacheSources {
	t.Helper()
	profile, err := contextplan.NewProviderProfile(c.Family(), c.provider.config.Model)
	if err != nil {
		t.Fatal(err)
	}
	profile, err = profile.WithInferenceSettings(c.provider.config.Inference)
	if err != nil {
		t.Fatal(err)
	}
	project, err := domain.NewEmptyProjectInstructionsSnapshot(32 << 10)
	if err != nil {
		t.Fatal(err)
	}
	view, err := c.ProjectActiveContext()
	if err != nil {
		t.Fatal(err)
	}
	_, footprint, err := c.ProjectContextWindow()
	if err != nil {
		t.Fatal(err)
	}
	input, err := contextplan.NewPlanningInput(profile, testsupport.ToolCatalog(t), project, view, footprint, contextplan.NewSamplingInput(), contextplan.DisabledBudget())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := contextplan.NewPlanner().Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := contextplan.NewCacheSources(plan)
	if err != nil {
		t.Fatal(err)
	}
	return sources
}
