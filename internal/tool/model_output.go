package tool

import (
	"easycode/internal/domain"
	"errors"
)

// ModelOutput 把真实冻结结果与独立反馈组合，不重写工具状态或调用身份。
type ModelOutput struct {
	result   InvocationResult
	feedback []domain.FrozenHookFeedback
	text     string
}

func NewModelOutput(result InvocationResult, feedback []domain.FrozenHookFeedback) (ModelOutput, error) {
	if result.Validate() != nil {
		return ModelOutput{}, errors.New("invalid model tool result")
	}
	if len(feedback) > 0 && result.Capability() != CapabilityPatch && result.Capability() != CapabilityWrite && result.Capability() != CapabilityExec {
		return ModelOutput{}, errors.New("post feedback capability is unsupported")
	}
	previous := -1
	digest := ""
	for _, f := range feedback {
		if f.Validate() != nil || f.Identity.InvocationID != string(result.InvocationID()) || f.Identity.Event != domain.HookPostToolUse || f.Identity.Ordinal <= previous || digest != "" && digest != f.Identity.PlanDigest {
			return ModelOutput{}, errors.New("invalid model tool feedback")
		}
		previous = f.Identity.Ordinal
		digest = f.Identity.PlanDigest
	}
	tail, err := RenderPostFeedback(feedback)
	if err != nil {
		return ModelOutput{}, err
	}
	if len(result.Preview().Text())+len(tail) > MaxModelPreviewBytes {
		return ModelOutput{}, errors.New("combined model output exceeds capability limit")
	}
	totalDelta := 0
	for _, f := range feedback {
		totalDelta += f.BudgetDelta
	}
	if totalDelta != len(tail) {
		return ModelOutput{}, errors.New("model output feedback budget mismatch")
	}
	return ModelOutput{result: result, feedback: append([]domain.FrozenHookFeedback(nil), feedback...), text: result.Preview().Text() + tail}, nil
}
func (v ModelOutput) Result() InvocationResult { return v.result }
func (v ModelOutput) Feedback() []domain.FrozenHookFeedback {
	return append([]domain.FrozenHookFeedback(nil), v.feedback...)
}
func (v ModelOutput) Text() string { return v.text }
func (v ModelOutput) Validate() error {
	expected, err := NewModelOutput(v.result, v.feedback)
	if err != nil || expected.text != v.text {
		return errors.New("invalid model tool output")
	}
	return nil
}
