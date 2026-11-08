package domain

import (
	"strings"
	"unicode/utf8"
)

const MaxHookFeedbackBytes = 4 << 10

// HookFeedbackCandidate 只保存通过严格输出策略且获显式披露授权的正文。
// 它与可持久化的执行统计分离，调用预算接纳前不能进入事实源。
type HookFeedbackCandidate struct{ content string }

func NewHookFeedbackCandidate(content string) (HookFeedbackCandidate, error) {
	v := HookFeedbackCandidate{content: content}
	if strings.TrimSpace(content) == "" || len(content) > MaxHookFeedbackBytes || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return HookFeedbackCandidate{}, HookOutputInvalid
	}
	return v, nil
}
func (v HookFeedbackCandidate) Content() string { return v.content }
func (v HookFeedbackCandidate) Empty() bool     { return v.content == "" }
func (v HookFeedbackCandidate) Validate() error {
	if v.Empty() {
		return nil
	}
	_, err := NewHookFeedbackCandidate(v.content)
	return err
}

// HookParsedOutput 是输出策略的纯内存结果，原始双流永不进入该值。
type HookParsedOutput struct {
	Status   HookOutcome
	Code     HookCode
	Feedback HookFeedbackCandidate
}

func NewHookParsedOutput(event HookEvent, policy HookFeedbackPolicy, status HookOutcome, code HookCode, feedback HookFeedbackCandidate) (HookParsedOutput, error) {
	v := HookParsedOutput{status, code, feedback}
	if !policy.ValidFor(event) || !status.ValidCode(code) || feedback.Validate() != nil || event == HookPostToolUse && status == HookBlock || !feedback.Empty() && (event != HookPostToolUse || policy != HookFeedbackModel || status != HookContinue) {
		return HookParsedOutput{}, HookConfigInvalid
	}
	return v, nil
}

// HookExecution 将无正文统计与待接纳反馈保持为两个明确字段。
type HookExecution struct {
	Result   HookRunResult
	Feedback HookFeedbackCandidate
}

func NewHookExecution(result HookRunResult, policy HookFeedbackPolicy, feedback HookFeedbackCandidate) (HookExecution, error) {
	v := HookExecution{result, feedback}
	if result.Validate() != nil {
		return HookExecution{}, HookConfigInvalid
	}
	_, err := NewHookParsedOutput(result.Identity.Event, policy, result.Status, result.Code, feedback)
	return v, err
}
