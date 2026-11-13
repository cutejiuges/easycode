package hook

import (
	"bytes"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"strings"
	"unicode/utf8"
)

// ParseExecution 按事件解析封闭输出；只有显式 model 策略可以返回正文候选。
func ParseExecution(event domain.HookEvent, policy domain.HookFeedbackPolicy, exit int, stdout []byte) domain.HookParsedOutput {
	result := domain.HookParsedOutput{Status: domain.HookError, Code: domain.HookOutputInvalid}
	if !policy.ValidFor(event) {
		result.Code = domain.HookConfigInvalid
		return result
	}
	if event == domain.HookStop {
		return parseStopOutput(policy, exit, stdout)
	}
	if event == domain.HookPreToolUse {
		result.Status, result.Code = ParseOutput(exit, stdout)
		return result
	}
	if len(stdout) > domain.MaxHookOutputBytes {
		result.Code = domain.HookOutputLimit
		return result
	}
	if exit != 0 {
		result.Code = domain.HookExitFailed
		return result
	}
	if !utf8.Valid(stdout) {
		return result
	}
	stdout = bytes.TrimSpace(stdout)
	if len(stdout) == 0 {
		result.Status, result.Code = domain.HookContinue, domain.HookOK
		return result
	}
	members, err := codec.ObjectMembers(stdout)
	if err != nil {
		return result
	}
	if len(members) == 0 {
		result.Status, result.Code = domain.HookContinue, domain.HookOK
		return result
	}
	var out struct {
		Specific struct {
			Event   domain.HookEvent `json:"hookEventName"`
			Content string           `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if codec.UnmarshalRecord(stdout, &out) != nil || out.Specific.Event != event || strings.TrimSpace(out.Specific.Content) == "" {
		return result
	}
	feedback, err := domain.NewHookFeedbackCandidate(out.Specific.Content)
	if err != nil {
		return result
	}
	if policy != domain.HookFeedbackModel {
		result.Code = domain.HookFeedbackNotAllowed
		return result
	}
	result.Status, result.Code, result.Feedback = domain.HookContinue, domain.HookOK, feedback
	return result
}
