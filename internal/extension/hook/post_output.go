package hook

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"strings"
	"unicode/utf8"
)

// ParseExecution 按事件解析封闭输出；只有显式 model 策略可以返回正文候选。
func ParseExecution(event domain.HookEvent, policy domain.HookFeedbackPolicy, exit int, stdout []byte, context domain.HookRewriteContext) domain.HookParsedOutput {
	result := domain.HookParsedOutput{Status: domain.HookError, Code: domain.HookOutputInvalid}
	if !policy.ValidFor(event) {
		result.Code = domain.HookConfigInvalid
		return result
	}
	if event != domain.HookPreToolUse && !context.Empty() {
		return result
	}
	if event == domain.HookStop {
		return parseStopOutput(policy, exit, stdout)
	}
	if event == domain.HookPreCompact {
		return parsePreCompactOutput(policy, exit, stdout)
	}
	if event == domain.HookPreToolUse {
		return ParseOutput(exit, stdout, context)
	}
	result, terminal := classifyHookExit(exit, stdout, false)
	if terminal {
		return result
	}
	if !utf8.Valid(stdout) {
		return result
	}
	stdout, members, err := hookOutputObject(stdout)
	if err != nil {
		return result
	}
	if len(members) == 0 {
		return domain.HookParsedOutput{Status: domain.HookContinue, Code: domain.HookOK}
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
