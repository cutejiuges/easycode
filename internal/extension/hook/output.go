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

// ParseOutput 只返回安全分类；脚本输出和自由文本原因不越过此边界。
func ParseOutput(exit int, stdout []byte) (domain.HookOutcome, domain.HookCode) {
	if len(stdout) > domain.MaxHookOutputBytes {
		return domain.HookError, domain.HookOutputLimit
	}
	if exit == 2 {
		return domain.HookBlock, domain.HookDenied
	}
	if exit != 0 {
		return domain.HookError, domain.HookExitFailed
	}
	if !utf8.Valid(stdout) {
		return domain.HookError, domain.HookOutputInvalid
	}
	stdout = bytes.TrimSpace(stdout)
	if len(stdout) == 0 {
		return domain.HookContinue, domain.HookOK
	}
	m, err := codec.ObjectMembers(stdout)
	if err != nil {
		return domain.HookError, domain.HookOutputInvalid
	}
	if len(m) == 0 {
		return domain.HookContinue, domain.HookOK
	}
	var out hookOutput
	if codec.UnmarshalRecord(stdout, &out) != nil || out.Specific.Event != "PreToolUse" || out.Specific.Decision != "deny" || strings.TrimSpace(out.Specific.Reason) == "" || len(out.Specific.Reason) > 4096 {
		return domain.HookError, domain.HookOutputInvalid
	}
	return domain.HookBlock, domain.HookDenied
}
