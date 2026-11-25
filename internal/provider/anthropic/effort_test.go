package anthropic

import (
	"bytes"
	"fmt"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/testsupport"
	"easycode/internal/secret"
)

func TestRuntimeEffortRequestPolicyGolden(t *testing.T) {
	var actual bytes.Buffer
	for _, mode := range []string{"disabled", "adaptive", "enabled"} {
		thinkingMode := domain.ThinkingMode(mode)
		thinkingBudget := uint64(0)
		thinkingDisplay := domain.ThinkingDisplay("")
		if thinkingMode == domain.ThinkingEnabled {
			thinkingBudget = 1024
		}
		if thinkingMode != domain.ThinkingDisabled {
			thinkingDisplay = domain.DisplayOmitted
		}
		thinking, _ := domain.NewThinkingSettings(thinkingMode, thinkingBudget, thinkingDisplay)
		startup, _ := domain.NewAnthropicInference(thinking, "high", 8192)
		for _, cache := range []domain.AnthropicCacheMode{domain.AnthropicCacheDisabled, domain.AnthropicCache5m, domain.AnthropicCache1h} {
			policy, _ := domain.NewCachePolicy(cache, domain.OpenAICacheOmit, domain.OpenAIHTTPSSE)
			instance, err := New(Config{Inference: startup, CachePolicy: policy, BaseURL: "https://example.invalid", Model: "fixture-model", APIKey: secret.New("fixture-key")})
			if err != nil {
				t.Fatal(err)
			}
			c := testConversation(t, instance)
			c.history.Append(testInputEntry(t, newUserMessage("same candidate input")))
			choices := []domain.EffortChoice{domain.NewResetEffortChoice(), domain.NewAutomaticEffortChoice()}
			for _, level := range []domain.EffortLevel{"low", "high"} {
				choice, _ := domain.NewExplicitEffortChoice(level)
				choices = append(choices, choice)
			}
			for _, choice := range choices {
				selected, err := choice.Apply(startup)
				if err != nil {
					t.Fatal(err)
				}
				input, err := testSamplingInput(t, selected).WithToolCatalog(testsupport.ToolCatalog(t))
				if err != nil {
					t.Fatal(err)
				}
				ordinary, err := c.planSamplingRequest(c.history.Snapshot(), input)
				if err != nil {
					t.Fatal(err)
				}
				compact, _ := provider.NewCompactionInput(input, "preserve failures", 4096)
				summary, err := c.planCompactionRequest(c.history.Snapshot(), compact)
				if err != nil {
					t.Fatal(err)
				}
				for _, plan := range []samplingRequestPlan{ordinary, summary} {
					body, err := plan.body(cache)
					if err != nil {
						t.Fatal(err)
					}
					evidence, err := plan.evidence()
					if err != nil {
						t.Fatal(err)
					}
					if instance.config.Inference != startup || selected.MaxOutputTokens() != startup.MaxOutputTokens() {
						t.Fatal("operation mutated startup")
					}
					name := fmt.Sprintf("%s/%s/%s/%s/%s", mode, cache, choice.Mode(), choice.Level(), plan.purpose)
					actual.Write(testsupport.EffortGoldenRecord(t, name, body, evidence, selected, plan.purpose))
				}
			}
			if err := instance.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	testsupport.CheckEffortGolden(t, actual.Bytes())
}
