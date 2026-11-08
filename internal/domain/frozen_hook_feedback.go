package domain

import (
	"easycode/internal/codec"
)

// HookFeedbackKind 区分明确保存的正文、安全分类与空选择。
type HookFeedbackKind string

const (
	HookFeedbackEmpty   HookFeedbackKind = "empty"
	HookFeedbackContent HookFeedbackKind = "content"
	HookFeedbackCode    HookFeedbackKind = "code"
)

// FrozenHookFeedback 是预算接纳后的单项事实，不含原始输出或命令。
type FrozenHookFeedback struct {
	Identity    HookRunIdentity  `json:"identity"`
	Reference   HookReference    `json:"reference"`
	Kind        HookFeedbackKind `json:"kind"`
	Content     string           `json:"content"`
	Code        HookCode         `json:"code"`
	BudgetDelta int              `json:"budget_delta"`
}

func NewFrozenHookFeedback(id HookRunIdentity, ref HookReference, kind HookFeedbackKind, content string, code HookCode, delta int) (FrozenHookFeedback, error) {
	v := FrozenHookFeedback{id, ref, kind, content, code, delta}
	return v, v.Validate()
}
func (v FrozenHookFeedback) Validate() error {
	if v.Identity.Validate() != nil || v.Reference.Validate() != nil || v.Identity.Event != v.Reference.Event || v.BudgetDelta < 0 || v.BudgetDelta > MaxHookOutputBytes {
		return HookConfigInvalid
	}
	switch v.Kind {
	case HookFeedbackEmpty:
		if v.Content != "" || v.Code != HookOK || v.BudgetDelta != 0 {
			return HookConfigInvalid
		}
	case HookFeedbackContent:
		if _, err := NewHookFeedbackCandidate(v.Content); err != nil || v.Code != HookOK || v.BudgetDelta == 0 {
			return HookConfigInvalid
		}
	case HookFeedbackCode:
		if v.Content != "" || !v.Code.Valid() || v.Code == HookOK || v.BudgetDelta == 0 {
			return HookConfigInvalid
		}
	default:
		return HookConfigInvalid
	}
	if v.Kind != HookFeedbackEmpty && (v.Identity.Event != HookPostToolUse || v.Reference.Feedback != HookFeedbackModel) {
		return HookConfigInvalid
	}
	return nil
}
func DecodeFrozenHookFeedback(b []byte) (FrozenHookFeedback, error) {
	var v FrozenHookFeedback
	if len(b) > MaxHookOutputBytes || codec.UnmarshalRecord(b, &v) != nil {
		return v, HookConfigInvalid
	}
	return v, v.Validate()
}

// HookFinished 保存一次执行统计及独立的已冻结反馈选择。
type HookFinished struct {
	Result   HookRunResult      `json:"result"`
	Feedback FrozenHookFeedback `json:"feedback"`
}

func NewHookFinished(result HookRunResult, feedback FrozenHookFeedback) (HookFinished, error) {
	v := HookFinished{result, feedback}
	return v, v.Validate()
}
func (v HookFinished) Validate() error {
	if v.Result.Validate() != nil || v.Feedback.Validate() != nil || v.Result.Identity != v.Feedback.Identity {
		return HookConfigInvalid
	}
	if v.Feedback.Kind == HookFeedbackContent && v.Result.Status != HookContinue || v.Feedback.Kind == HookFeedbackCode && (v.Result.Status == HookContinue || v.Result.Code != v.Feedback.Code) {
		return HookConfigInvalid
	}
	if v.Result.Origin == "recovery" && v.Feedback.Kind != HookFeedbackEmpty {
		return HookConfigInvalid
	}
	return nil
}
func DecodeHookFinished(b []byte) (HookFinished, error) {
	var v HookFinished
	if len(b) > MaxHookConfigBytes || codec.UnmarshalRecord(b, &v) != nil {
		return v, HookConfigInvalid
	}
	return v, v.Validate()
}

// NewEmptyHookFinished 明确冻结未披露正文的选择，不省略当前记录字段。
func NewEmptyHookFinished(result HookRunResult, ref HookReference) (HookFinished, error) {
	feedback, err := NewFrozenHookFeedback(result.Identity, ref, HookFeedbackEmpty, "", HookOK, 0)
	if err != nil {
		return HookFinished{}, err
	}
	return NewHookFinished(result, feedback)
}
