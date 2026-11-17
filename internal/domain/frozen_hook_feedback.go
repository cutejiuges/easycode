package domain

import (
	"bytes"
	"easycode/internal/codec"
	"encoding/json"
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
	if v.Kind != HookFeedbackEmpty && (!v.Identity.Event.Post() || v.Reference.Feedback != HookFeedbackModel) {
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
	Result      HookRunResult      `json:"result"`
	Feedback    FrozenHookFeedback `json:"feedback"`
	InputEffect HookInputEffect    `json:"-"`
}

func NewHookFinished(result HookRunResult, feedback FrozenHookFeedback) (HookFinished, error) {
	v := HookFinished{Result: result, Feedback: feedback}
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
	if v.Result.Identity.Event == HookPreToolUse {
		_, capability, ok := v.Result.Identity.Target.ToolCall()
		if !ok || v.InputEffect.Validate(capability) != nil || v.Feedback.Kind != HookFeedbackEmpty {
			return HookConfigInvalid
		}
		if v.InputEffect.Kind() == HookInputReplaced && (v.Result.Origin != "execution" || v.Result.Status != HookContinue || v.Result.Code != HookOK || !v.Result.ExitKnown || v.Result.ExitCode != 0 || v.Result.Cancelled || v.Result.TimedOut || !v.Result.CleanupConfirmed) {
			return HookConfigInvalid
		}
	} else if !v.InputEffect.Empty() {
		return HookConfigInvalid
	}
	b, err := v.MarshalJSON()
	if err != nil || len(b) > MaxHookConfigBytes {
		return HookConfigInvalid
	}
	return nil
}

// NewPreHookFinished 显式冻结输入证据，不能把缺失字段按无变化兼容读取。
func NewPreHookFinished(result HookRunResult, ref HookReference, effect HookInputEffect) (HookFinished, error) {
	feedback, err := NewFrozenHookFeedback(result.Identity, ref, HookFeedbackEmpty, "", HookOK, 0)
	if err != nil || result.Identity.Event != HookPreToolUse {
		return HookFinished{}, HookConfigInvalid
	}
	v := HookFinished{Result: result, Feedback: feedback, InputEffect: effect.Clone()}
	return v, v.Validate()
}
func (v HookFinished) Clone() HookFinished { v.InputEffect = v.InputEffect.Clone(); return v }
func (v HookFinished) Equal(other HookFinished) bool {
	if v.Validate() != nil || other.Validate() != nil {
		return false
	}
	left, le := v.MarshalJSON()
	right, re := other.MarshalJSON()
	return le == nil && re == nil && bytes.Equal(left, right)
}
func (v HookFinished) MarshalJSON() ([]byte, error) {
	var effect json.RawMessage
	if v.Result.Identity.Event == HookPreToolUse {
		encoded, err := v.InputEffect.MarshalJSON()
		if err != nil {
			return nil, err
		}
		effect = encoded
	}
	resultTarget, err := v.Result.Identity.Target.MarshalJSON()
	if err != nil {
		return nil, err
	}
	feedbackTarget := resultTarget
	if v.Feedback.Identity.Target != v.Result.Identity.Target {
		feedbackTarget, err = v.Feedback.Identity.Target.MarshalJSON()
		if err != nil {
			return nil, err
		}
	}
	// 两份身份的其余字段仍分别编码；只复用相等且已验证的目标字节，不修复非法的身份差异。
	return codec.MarshalStable(struct {
		Result      hookFinishedResult   `json:"result"`
		Feedback    hookFinishedFeedback `json:"feedback"`
		InputEffect json.RawMessage      `json:"input_effect,omitempty"`
	}{
		Result: hookFinishedResult{
			Identity: finishedIdentity(v.Result.Identity, resultTarget), Status: v.Result.Status, Code: v.Result.Code,
			ExitKnown: v.Result.ExitKnown, ExitCode: v.Result.ExitCode, TimedOut: v.Result.TimedOut,
			Cancelled: v.Result.Cancelled, StdoutBytes: v.Result.StdoutBytes, StderrBytes: v.Result.StderrBytes,
			CleanupConfirmed: v.Result.CleanupConfirmed, Origin: v.Result.Origin,
		},
		Feedback: hookFinishedFeedback{
			Identity: finishedIdentity(v.Feedback.Identity, feedbackTarget), Reference: v.Feedback.Reference,
			Kind: v.Feedback.Kind, Content: v.Feedback.Content, Code: v.Feedback.Code, BudgetDelta: v.Feedback.BudgetDelta,
		},
		InputEffect: effect,
	})
}
func DecodeHookFinished(b []byte) (HookFinished, error) {
	if len(b) > MaxHookConfigBytes {
		return HookFinished{}, HookConfigInvalid
	}
	members, err := codec.ObjectMembers(b)
	if err != nil {
		return HookFinished{}, HookConfigInvalid
	}
	result, err := DecodeHookRunResult(members["result"])
	if err != nil {
		return HookFinished{}, HookConfigInvalid
	}
	var wire struct {
		Result      HookRunResult      `json:"result"`
		Feedback    FrozenHookFeedback `json:"feedback"`
		InputEffect json.RawMessage    `json:"input_effect,omitempty"`
	}
	if codec.UnmarshalRecord(b, &wire) != nil {
		return HookFinished{}, HookConfigInvalid
	}
	_, has := members["input_effect"]
	if has != (result.Identity.Event == HookPreToolUse) {
		return HookFinished{}, HookConfigInvalid
	}
	v := HookFinished{Result: result, Feedback: wire.Feedback}
	if has {
		_, capability, _ := result.Identity.Target.ToolCall()
		v.InputEffect, err = DecodeHookInputEffect(capability, wire.InputEffect)
		if err != nil {
			return HookFinished{}, HookConfigInvalid
		}
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
