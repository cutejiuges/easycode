package hook

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

type hookOutput struct {
	Specific struct {
		Event    string `json:"hookEventName"`
		Decision string `json:"permissionDecision"`
		Reason   string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

// ParseOutput 只返回封闭候选与安全分类，原始输出和自由文本原因不越过此边界。
func ParseOutput(exit int, stdout []byte, context domain.HookRewriteContext) domain.HookParsedOutput {
	result := domain.HookParsedOutput{Status: domain.HookError, Code: domain.HookOutputInvalid}
	if len(stdout) > domain.MaxHookOutputBytes {
		result.Code = domain.HookOutputLimit
		return result
	}
	if exit == 2 {
		result.Status, result.Code = domain.HookBlock, domain.HookDenied
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
	m, err := codec.ObjectMembers(stdout)
	if err != nil {
		return result
	}
	if len(m) == 0 {
		result.Status, result.Code = domain.HookContinue, domain.HookOK
		return result
	}
	if len(m) != 1 {
		return result
	}
	specific, err := codec.ObjectMembers(m["hookSpecificOutput"])
	if err != nil {
		return result
	}
	if raw, present := specific["updatedInput"]; present {
		var event string
		if len(specific) != 2 || codec.UnmarshalRecord(specific["hookEventName"], &event) != nil || event != "PreToolUse" {
			return result
		}
		replacement, err := domain.DecodeHookReplacement(context, raw)
		if err != nil {
			return result
		}
		result.Status, result.Code, result.Replacement = domain.HookContinue, domain.HookOK, replacement
		return result
	}
	var out hookOutput
	if codec.UnmarshalRecord(stdout, &out) != nil || out.Specific.Event != "PreToolUse" || out.Specific.Decision != "deny" || strings.TrimSpace(out.Specific.Reason) == "" || len(out.Specific.Reason) > 4096 {
		return result
	}
	result.Status, result.Code = domain.HookBlock, domain.HookDenied
	return result
}
