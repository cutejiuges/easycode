package hook

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

type preCompactOutput struct {
	Continue *bool                     `json:"continue,omitempty"`
	Specific *preCompactSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

type preCompactSpecificOutput struct {
	Event        domain.HookEvent `json:"hookEventName"`
	Instructions string           `json:"summaryInstructions"`
}

// parsePreCompactOutput 只在正常退出时接纳当前事件的结构化控制及显式授权的摘要重点。
func parsePreCompactOutput(policy domain.HookFeedbackPolicy, exit int, stdout []byte) domain.HookParsedOutput {
	result, terminal := classifyHookExit(exit, stdout, false)
	if terminal {
		return result
	}
	if !utf8.Valid(stdout) {
		return result
	}
	stdout = bytes.TrimSpace(stdout)
	var output preCompactOutput
	if len(stdout) > 0 && codec.UnmarshalRecord(stdout, &output) != nil {
		return result
	}
	if output.Continue != nil && !*output.Continue {
		if output.Specific == nil {
			result.Status, result.Code = domain.HookBlock, domain.HookDenied
		}
		return result
	}
	if output.Specific != nil {
		if output.Specific.Event != domain.HookPreCompact {
			return result
		}
		focus, err := domain.NewHookFeedbackCandidate(strings.TrimSpace(output.Specific.Instructions))
		if err != nil {
			return result
		}
		if policy != domain.HookFeedbackModel {
			result.Code = domain.HookFeedbackNotAllowed
			return result
		}
		result.Feedback = focus
	}
	result.Status, result.Code = domain.HookContinue, domain.HookOK
	return result
}
