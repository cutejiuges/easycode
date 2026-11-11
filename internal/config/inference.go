package config

import (
	"encoding/json"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/fault"
)

// inferenceFields 在环境覆盖前保留用户选择，不预先绑定 family。
type inferenceFields struct {
	thinking        domain.ThinkingSettings
	anthropicEffort domain.AnthropicEffort
	openaiEffort    domain.OpenAIEffort
	summary         domain.ReasoningSummary
	output          uint64
}

func decodeThinking(raw json.RawMessage) (domain.ThinkingSettings, error) {
	members, err := codec.ObjectMembers(raw)
	if err != nil {
		return domain.ThinkingSettings{}, invalidInference()
	}
	var mode string
	var display string
	var budget optionalUint64
	for key, value := range members {
		switch key {
		case "type":
			mode, err = decodeStringField(value)
		case "display":
			display, err = decodeStringField(value)
			if display == "" {
				return domain.ThinkingSettings{}, invalidInference()
			}
		case "budget_tokens":
			budget, err = decodeUint64Field(value)
		default:
			return domain.ThinkingSettings{}, invalidInference()
		}
		if err != nil {
			return domain.ThinkingSettings{}, invalidInference()
		}
	}
	if mode == "" || (budget.present && mode != "enabled") {
		return domain.ThinkingSettings{}, invalidInference()
	}
	if _, present := members["display"]; present && mode != "adaptive" && mode != "enabled" {
		return domain.ThinkingSettings{}, invalidInference()
	}
	thinking, err := domain.NewThinkingSettings(domain.ThinkingMode(mode), budget.value, domain.ThinkingDisplay(display))
	if err != nil {
		return domain.ThinkingSettings{}, invalidInference()
	}
	return thinking, nil
}

func invalidInference() error {
	return fault.New(fault.CodeInvalidConfiguration, "inference configuration is invalid")
}

// InferenceSettings 在最终 family 确定后解析冻结策略，调用不读取文件或环境。
func (config Config) InferenceSettings() (domain.InferenceSettings, error) {
	f := config.inference
	var settings domain.InferenceSettings
	var err error
	switch config.Provider.Family {
	case domain.ProviderAnthropic:
		if f.openaiEffort != "" || f.summary != "" {
			return settings, invalidInference()
		}
		settings, err = domain.NewAnthropicInference(f.thinking, f.anthropicEffort, f.output)
	case domain.ProviderOpenAI:
		if f.thinking.Mode() != domain.ThinkingOmit || f.anthropicEffort != "" {
			return settings, invalidInference()
		}
		settings, err = domain.NewOpenAIInference(f.openaiEffort, f.summary, f.output)
	default:
		return settings, invalidInference()
	}
	if err != nil {
		return domain.InferenceSettings{}, invalidInference()
	}
	if config.ContextBudget.Enabled() {
		if config.Provider.Family == domain.ProviderOpenAI && settings.HasReasoning() && f.output == 0 {
			return domain.InferenceSettings{}, invalidInference()
		}
		if (f.output != 0 || settings.HasReasoning()) && config.ContextBudget.ReservedOutput() < settings.MaxOutputTokens() {
			return domain.InferenceSettings{}, invalidInference()
		}
	}
	return settings, nil
}
