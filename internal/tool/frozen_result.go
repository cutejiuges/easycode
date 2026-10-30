package tool

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// ValidateFrozenResult 检查进入 Session/Provider 的最终事实，禁止候选材料夹带模型不可见正文。
func ValidateFrozenResult(result InvocationResult) error {
	if err := result.Validate(); err != nil {
		return err
	}
	var lines []string
	switch result.capability {
	case CapabilityGlob:
		lines = append(lines, result.globMetadata.matches...)
	case CapabilityGrep:
		for _, match := range result.grepMetadata.matches {
			lines = append(lines, RenderGrepMatch(result.grepMetadata.mode, match))
		}
	default:
		return nil
	}
	if len(lines) > 1000 {
		return errors.New("too many frozen search matches")
	}
	total := 0
	for _, line := range lines {
		if utf8.RuneCountInString(line) > MaxSearchLineCodePoints || !strings.Contains(result.preview.text, line+"\n") {
			return errors.New("frozen search metadata contains hidden text")
		}
		total += len(line) + 1
	}
	if total > len(result.preview.text) {
		return errors.New("frozen search metadata exceeds visible body")
	}
	return nil
}
