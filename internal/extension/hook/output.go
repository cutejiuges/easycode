package hook

import (
	"bytes"
	"encoding/json"
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
	result, terminal := classifyHookExit(exit, stdout, true)
	if terminal {
		return result
	}
	if !utf8.Valid(stdout) {
		return result
	}
	stdout, m, err := hookOutputObject(stdout)
	if err != nil {
		return result
	}
	if len(m) == 0 {
		return domain.HookParsedOutput{Status: domain.HookContinue, Code: domain.HookOK}
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

// classifyHookExit 先按 stdout 上限和进程退出码分类，保持各事件的阻断约定。
func classifyHookExit(exit int, stdout []byte, blockOnExitTwo bool) (domain.HookParsedOutput, bool) {
	switch {
	case len(stdout) > domain.MaxHookOutputBytes:
		return domain.HookParsedOutput{Status: domain.HookError, Code: domain.HookOutputLimit}, true
	case exit == 2 && blockOnExitTwo:
		return domain.HookParsedOutput{Status: domain.HookBlock, Code: domain.HookDenied}, true
	case exit != 0:
		return domain.HookParsedOutput{Status: domain.HookError, Code: domain.HookExitFailed}, true
	default:
		return domain.HookParsedOutput{Status: domain.HookError, Code: domain.HookOutputInvalid}, false
	}
}

func hookOutputObject(stdout []byte) ([]byte, map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		return trimmed, nil, nil
	}
	members, err := codec.ObjectMembers(trimmed)
	return trimmed, members, err
}
