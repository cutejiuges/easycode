package hook

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
)

// parseStopOutput 只接纳显式正常退出的结构化阻断，不转发原始双流。
func parseStopOutput(policy domain.HookFeedbackPolicy, exit int, stdout []byte) domain.HookParsedOutput {
	v, terminal := classifyHookExit(exit, stdout, true)
	if terminal {
		return v
	}
	trimmed, m, err := hookOutputObject(stdout)
	if err != nil {
		return v
	}
	if len(m) == 0 {
		return domain.HookParsedOutput{Status: domain.HookContinue, Code: domain.HookOK}
	}
	var block struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if codec.DecodeObject(trimmed, &block, "decision", "reason") != nil || block.Decision != "block" {
		return v
	}
	candidate, err := domain.NewHookFeedbackCandidate(block.Reason)
	if err != nil {
		return v
	}
	v.Status, v.Code = domain.HookBlock, domain.HookDenied
	if policy == domain.HookFeedbackModel {
		v.Feedback = candidate
	}
	return v
}
