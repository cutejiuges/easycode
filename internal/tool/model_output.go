package tool

import (
	"easycode/internal/domain"
	"errors"
)

// ModelOutput 把真实冻结结果与独立反馈组合，不重写工具状态或调用身份。
type ModelOutput struct {
	result    InvocationResult
	feedback  []domain.FrozenHookFeedback
	text      string
	selection PostHookSelection
	post      bool
	evidence  InputResultEvidence
}

func NewModelOutput(result InvocationResult, evidence InputResultEvidence) (ModelOutput, error) {
	return newModelOutput(result, evidence, nil)
}

// NewPostModelOutput 先验证所选计划的调用归属，再组合冻结的工具数据。
func NewPostModelOutput(result InvocationResult, evidence InputResultEvidence, selection PostHookSelection, feedback []domain.FrozenHookFeedback) (ModelOutput, error) {
	if err := selection.validate(result, feedback); err != nil {
		return ModelOutput{}, err
	}
	if evidence.admission != selection.admission {
		return ModelOutput{}, errors.New("post and input admission differ")
	}
	v, err := newModelOutput(result, evidence, feedback)
	v.selection = selection
	v.post = true
	return v, err
}

func newModelOutput(result InvocationResult, evidence InputResultEvidence, feedback []domain.FrozenHookFeedback) (ModelOutput, error) {
	if result.Validate() != nil || evidence.ValidateResult(result) != nil {
		return ModelOutput{}, errors.New("invalid model tool result")
	}
	if len(feedback) > 0 && result.Capability() != CapabilityPatch && result.Capability() != CapabilityWrite && result.Capability() != CapabilityExec {
		return ModelOutput{}, errors.New("post feedback capability is unsupported")
	}
	previous := -1
	digest := ""
	encodedBytes := 0
	for i, f := range feedback {
		if f.Validate() != nil || f.Identity.InvocationID() != string(result.InvocationID()) || !f.Identity.Event.Post() || f.Identity.Ordinal <= previous || digest != "" && digest != f.Identity.PlanDigest {
			return ModelOutput{}, errors.New("invalid model tool feedback")
		}
		previous = f.Identity.Ordinal
		digest = f.Identity.PlanDigest
		prefix, err := RenderPostFeedback(feedback[:i+1])
		if err != nil || f.BudgetDelta != len(prefix)-encodedBytes {
			return ModelOutput{}, errors.New("model output feedback segment mismatch")
		}
		encodedBytes = len(prefix)
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
	return ModelOutput{result: result, evidence: evidence.Clone(), feedback: append([]domain.FrozenHookFeedback(nil), feedback...), text: result.Preview().Text() + tail}, nil
}
func (v ModelOutput) Evidence() InputResultEvidence { return v.evidence.Clone() }
func (v ModelOutput) Result() InvocationResult      { return v.result }
func (v ModelOutput) Feedback() []domain.FrozenHookFeedback {
	return append([]domain.FrozenHookFeedback(nil), v.feedback...)
}
func (v ModelOutput) Text() string { return v.text }
func (v ModelOutput) Validate() error {
	expected, err := newModelOutput(v.result, v.evidence, v.feedback)
	if v.post {
		if v.evidence.admission != v.selection.admission {
			return errors.New("post and input admission differ")
		}
		if e := v.selection.validate(v.result, v.feedback); e != nil {
			return e
		}
	} else if len(v.feedback) > 0 {
		return errors.New("post feedback lacks selection evidence")
	}
	if err != nil || expected.text != v.text {
		return errors.New("invalid model tool output")
	}
	return nil
}
