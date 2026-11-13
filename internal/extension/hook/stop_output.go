package hook

import (
	"bytes"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// parseStopOutput 只接纳显式正常退出的结构化阻断，不转发原始双流。
func parseStopOutput(policy domain.HookFeedbackPolicy, exit int, stdout []byte) domain.HookParsedOutput {
	v := domain.HookParsedOutput{Status: domain.HookError, Code: domain.HookOutputInvalid}
	if len(stdout) > domain.MaxHookOutputBytes {
		v.Code = domain.HookOutputLimit
		return v
	}
	if exit == 2 {
		v.Status, v.Code = domain.HookBlock, domain.HookDenied
		return v
	}
	if exit != 0 {
		v.Code = domain.HookExitFailed
		return v
	}
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		v.Status, v.Code = domain.HookContinue, domain.HookOK
		return v
	}
	m, err := codec.ObjectMembers(trimmed)
	if err != nil {
		return v
	}
	if len(m) == 0 {
		v.Status, v.Code = domain.HookContinue, domain.HookOK
		return v
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
