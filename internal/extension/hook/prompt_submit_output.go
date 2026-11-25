package hook

import (
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// parseUserPromptSubmitOutput 仅识别本地放行与阻断，丢弃自由文本。
func parseUserPromptSubmitOutput(exit int, stdout []byte) domain.HookParsedOutput {
	result, terminal := classifyHookExit(exit, stdout, true)
	if terminal || !utf8.Valid(stdout) {
		return result
	}
	stdout, members, err := hookOutputObject(stdout)
	if err != nil {
		return result
	}
	if len(members) == 0 {
		return domain.HookParsedOutput{Status: domain.HookContinue, Code: domain.HookOK}
	}
	var output struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason,omitempty"`
	}
	if codec.DecodeObject(stdout, &output, "decision") != nil || output.Decision != "block" {
		return result
	}
	return domain.HookParsedOutput{Status: domain.HookBlock, Code: domain.HookDenied}
}
