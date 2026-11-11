package tui

import (
	"easycode/internal/domain"
	"fmt"
)

// WithInferenceSettings 由应用装配同一冻结值，不读取配置或改变请求。
func (model Model) WithInferenceSettings(settings domain.InferenceSettings) Model {
	if settings.Validate() == nil {
		model.inference = settings
	}
	return model
}

func (model Model) inferenceLines() []string {
	s := model.inference
	if s.Validate() != nil {
		return []string{"Inference: unavailable"}
	}
	choice := func(value string) string {
		if value == "" {
			return "unset · not sent (provider default)"
		}
		return value + " (explicit)"
	}
	lines := []string{"Inference · " + string(s.Family()) + " · frozen at startup"}
	if s.Family() == domain.ProviderAnthropic {
		t := s.Thinking()
		lines = append(lines, "Thinking: "+choice(string(t.Mode())))
		if t.Mode() == domain.ThinkingEnabled {
			lines = append(lines, fmt.Sprintf("Thinking budget: %d (explicit)", t.BudgetTokens()))
		}
		lines = append(lines, "Display: "+choice(string(t.Display())), "Effort: "+choice(string(s.AnthropicEffort())))
	} else {
		lines = append(lines, "Effort: "+choice(string(s.OpenAIEffort())), "Summary: "+choice(string(s.Summary())))
	}
	if value, explicit := s.ExplicitOutput(); explicit {
		lines = append(lines, fmt.Sprintf("Ordinary output cap: %d (explicit)", value))
	} else if s.Family() == domain.ProviderAnthropic {
		lines = append(lines, fmt.Sprintf("Ordinary output cap: %d (default)", s.MaxOutputTokens()))
	} else {
		lines = append(lines, "Ordinary output cap: server default / unknown (not sent)")
	}
	return append(lines, "Configured; endpoint support not verified", "Returned text and usage are independent observations")
}
