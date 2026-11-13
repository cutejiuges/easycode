package domain

import (
	"fmt"
	"unicode/utf8"

	"easycode/internal/codec"
)

// AssistantSampleText 是指定成功采样的可见正文，允许精确的空正文。
type AssistantSampleText struct {
	reference AssistantSampleReference
	text      string
}

func NewAssistantSampleText(reference AssistantSampleReference, text string) (AssistantSampleText, error) {
	if reference.Validate() != nil || !utf8.ValidString(text) {
		return AssistantSampleText{}, fmt.Errorf("invalid assistant sample text")
	}
	return AssistantSampleText{reference, text}, nil
}
func (v AssistantSampleText) Reference() AssistantSampleReference { return v.reference }
func (v AssistantSampleText) Text() string                        { return v.text }
func (v AssistantSampleText) Validate() error {
	_, err := NewAssistantSampleText(v.reference, v.text)
	return err
}

// HookMessageSource 是只读历史中的完成检查来源，不携带工具身份或原始输出。
type HookMessageSource struct {
	Identity  HookRunIdentity `json:"identity"`
	Reference HookReference   `json:"reference"`
}

func NewHookMessageSource(feedback StopFeedback) (HookMessageSource, error) {
	if feedback.Validate() != nil {
		return HookMessageSource{}, HookConfigInvalid
	}
	return HookMessageSource{feedback.Identity, feedback.Reference}, nil
}
func (v HookMessageSource) Validate() error {
	if v.Identity.Validate() != nil || v.Identity.Event != HookStop || v.Reference.Validate() != nil || v.Reference.Event != HookStop || v.Reference.Feedback != HookFeedbackModel {
		return HookConfigInvalid
	}
	return nil
}

// DecodeHookMessageSource 拒绝缺失的真实来源字段与额外私有数据。
func DecodeHookMessageSource(b []byte) (HookMessageSource, error) {
	type fields HookMessageSource
	var v fields
	if len(b) > MaxHookConfigBytes || codec.UnmarshalRecord(b, &v) != nil {
		return HookMessageSource{}, HookConfigInvalid
	}
	value := HookMessageSource(v)
	return value, value.Validate()
}
func (v *HookMessageSource) UnmarshalJSON(b []byte) error {
	next, err := DecodeHookMessageSource(b)
	if err != nil {
		return err
	}
	*v = next
	return nil
}
