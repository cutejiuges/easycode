package app

import (
	"fmt"
	"strings"

	"easycode/internal/domain"
	"easycode/internal/session"
	"easycode/internal/tool"
)

// preHookDetail 只读取已验证内存事实，保留 O、每个 C 和最终选择的不同含义。
func preHookDetail(entry session.ToolHistoryEntry) (string, error) {
	g := entry.PreHooks
	if g == nil || g.Validate(entry.Invocation) != nil {
		return "", fmt.Errorf("invalid pre hook detail")
	}
	input, err := tool.HookInputForInvocation(entry.Invocation)
	if err != nil {
		return "", err
	}
	encoded, err := input.MarshalJSON()
	if err != nil {
		return "", err
	}
	var text strings.Builder
	text.WriteString("PreToolUse · original request O\n")
	text.Write(encoded)
	profile, err := tool.HookProfileForInvocation(entry.Invocation)
	if err != nil {
		return "", err
	}
	for i, f := range g.Finished {
		context, contextErr := domain.NewHookRewriteContext(input, profile)
		if contextErr != nil {
			return "", contextErr
		}
		input, err = f.InputEffect.ApplyAgainst(context)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&text, "\n\nSaved candidate C%d · %s:%s · %s (%s)\ninput %s · stdout %d bytes · stderr %d bytes\n", i+1, f.Feedback.Reference.Source, f.Feedback.Reference.ID, f.Result.Status, f.Result.Code, f.InputEffect.AfterDigest(), f.Result.StdoutBytes, f.Result.StderrBytes)
		encoded, err = input.MarshalJSON()
		if err != nil {
			return "", err
		}
		text.Write(encoded)
	}
	if g.Decision == nil {
		text.WriteString("\n\nChecking · candidate has not been selected. Tool not admitted.")
	} else if g.Decision.Status != domain.HookContinue {
		fmt.Fprintf(&text, "\n\nGate rejected (%s) · candidate not selected; tool not executed.", g.Decision.Code)
	} else {
		text.WriteString("\n\nGate selected effective input E\n")
		encoded, err = input.MarshalJSON()
		if err != nil {
			return "", err
		}
		text.Write(encoded)
		if entry.Admission.Sequence() == 0 {
			text.WriteString("\nTool not admitted; ordinary permission still applies.")
		} else {
			fmt.Fprintf(&text, "\nExecution attempt admitted at sequence %d. Admission does not confirm process startup.", entry.Admission.Sequence())
		}
	}
	text.WriteString("\n\nRaw hook output and reasons were not recorded.")
	return text.String(), nil
}
