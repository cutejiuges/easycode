package context

import (
	"fmt"

	"easycode/internal/domain"
)

// ValidateInference 共用启动与运行中设置的固定预算约束，不估算历史。
func (budget Budget) ValidateInference(settings domain.InferenceSettings) error {
	if err := budget.Validate(); err != nil {
		return err
	}
	if err := settings.Validate(); err != nil {
		return err
	}
	if !budget.Enabled() {
		return nil
	}
	_, explicit := settings.ExplicitOutput()
	if settings.HasReasoning() && settings.MaxOutputTokens() == 0 {
		return fmt.Errorf("reasoning requires an explicit output cap with a context window")
	}
	if (explicit || settings.HasReasoning()) && budget.ReservedOutput() < settings.MaxOutputTokens() {
		return fmt.Errorf("inference output exceeds the reserved budget")
	}
	return nil
}

// AllowedEffortLevels 只声明当前冻结组合允许的档位，不探测远端。
func AllowedEffortLevels(startup domain.InferenceSettings, budget Budget) ([]domain.EffortLevel, error) {
	if err := budget.ValidateInference(startup); err != nil {
		return nil, err
	}
	levels, err := domain.EffortLevels(startup.Family())
	if err != nil {
		return nil, err
	}
	allowed := make([]domain.EffortLevel, 0, len(levels))
	for _, level := range levels {
		choice, err := domain.NewExplicitEffortChoice(level)
		if err != nil {
			return nil, err
		}
		settings, err := choice.Apply(startup)
		if err == nil && budget.ValidateInference(settings) == nil {
			allowed = append(allowed, level)
		}
	}
	return allowed, nil
}

// InferenceSettings 返回 profile 的不可变策略。
func (profile ProviderProfile) InferenceSettings() domain.InferenceSettings { return profile.inference }
