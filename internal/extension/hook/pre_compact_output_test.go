package hook

import (
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestPreCompactOutputUsesStrictScopedControl(t *testing.T) {
	const focusOutput = `{"hookSpecificOutput":{"hookEventName":"PreCompact","summaryInstructions":"  保留测试\n "}}`
	for _, tc := range []struct {
		name   string
		policy domain.HookFeedbackPolicy
		exit   int
		body   string
		status domain.HookOutcome
		code   domain.HookCode
		focus  string
	}{
		{"blank", domain.HookFeedbackNone, 0, " \n", domain.HookContinue, domain.HookOK, ""},
		{"empty", domain.HookFeedbackNone, 0, `{}`, domain.HookContinue, domain.HookOK, ""},
		{"allow", domain.HookFeedbackNone, 0, `{"continue":true}`, domain.HookContinue, domain.HookOK, ""},
		{"deny", domain.HookFeedbackNone, 0, `{"continue":false}`, domain.HookBlock, domain.HookDenied, ""},
		{"deny requires normal exit", domain.HookFeedbackNone, 2, `{"continue":false}`, domain.HookError, domain.HookExitFailed, ""},
		{"explicit focus", domain.HookFeedbackModel, 0, focusOutput, domain.HookContinue, domain.HookOK, "保留测试"},
		{"explicit focus with continue", domain.HookFeedbackModel, 0, `{"continue":true,` + focusOutput[1:], domain.HookContinue, domain.HookOK, "保留测试"},
		{"undisclosed focus", domain.HookFeedbackNone, 0, focusOutput, domain.HookError, domain.HookFeedbackNotAllowed, ""},
		{"oversized focus", domain.HookFeedbackModel, 0, strings.Replace(focusOutput, "保留测试", strings.Repeat("a", domain.MaxHookFeedbackBytes+1), 1), domain.HookError, domain.HookOutputInvalid, ""},
		{"oversized raw output", domain.HookFeedbackModel, 0, strings.Repeat(" ", domain.MaxHookOutputBytes+1), domain.HookError, domain.HookOutputLimit, ""},
		{"duplicate decision", domain.HookFeedbackModel, 0, `{"continue":true,"continue":false}`, domain.HookError, domain.HookOutputInvalid, ""},
		{"mixed deny and focus", domain.HookFeedbackModel, 0, `{"continue":false,` + focusOutput[1:], domain.HookError, domain.HookOutputInvalid, ""},
		{"unsupported stop reason", domain.HookFeedbackModel, 0, `{"continue":false,"stopReason":"private"}`, domain.HookError, domain.HookOutputInvalid, ""},
		{"foreign feedback", domain.HookFeedbackModel, 0, strings.Replace(focusOutput, "summaryInstructions", "additionalContext", 1), domain.HookError, domain.HookOutputInvalid, ""},
		{"null decision", domain.HookFeedbackModel, 0, `{"continue":null}`, domain.HookError, domain.HookOutputInvalid, ""},
		{"raw text", domain.HookFeedbackModel, 0, "ineligible text", domain.HookError, domain.HookOutputInvalid, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseExecution(domain.HookPreCompact, tc.policy, tc.exit, []byte(tc.body), domain.HookRewriteContext{})
			if got.Status != tc.status || got.Code != tc.code || got.Feedback.Content() != tc.focus || !got.Replacement.Empty() {
				t.Fatalf("output = %+v", got)
			}
		})
	}
	for _, event := range []domain.HookEvent{domain.HookStop, domain.HookPostToolUse, domain.HookPostToolUseFailure} {
		got := ParseExecution(event, domain.HookFeedbackModel, 0, []byte(focusOutput), domain.HookRewriteContext{})
		if got.Code != domain.HookOutputInvalid || !got.Feedback.Empty() {
			t.Fatalf("%s accepted PreCompact feedback: %+v", event, got)
		}
	}
}

func TestSessionStartOutputRequiresExplicitCanonicalContext(t *testing.T) {
	const output = `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"  保留正文\n "}}`
	for _, tc := range []struct {
		policy  domain.HookFeedbackPolicy
		exit    int
		input   string
		code    domain.HookCode
		context string
	}{
		{domain.HookFeedbackModel, 0, output, domain.HookOK, "  保留正文\n "},
		{domain.HookFeedbackNone, 0, output, domain.HookFeedbackNotAllowed, ""},
		{domain.HookFeedbackModel, 2, output, domain.HookExitFailed, ""},
		{domain.HookFeedbackNone, 0, " \n{}", domain.HookOK, ""},
		{domain.HookFeedbackModel, 0, `{"continue":false,` + output[1:], domain.HookOutputInvalid, ""},
		{domain.HookFeedbackModel, 0, strings.Replace(output, `"additionalContext":`, `"watchPaths":[],"additionalContext":`, 1), domain.HookOutputInvalid, ""},
		{domain.HookFeedbackModel, 0, strings.Replace(output, "SessionStart", "PostCompact", 1), domain.HookOutputInvalid, ""},
		{domain.HookFeedbackModel, 0, strings.Replace(output, "保留正文", strings.Repeat("x", domain.MaxHookFeedbackBytes), 1), domain.HookOutputInvalid, ""},
		{domain.HookFeedbackModel, 0, "fixture-private-stdout", domain.HookOutputInvalid, ""},
	} {
		got := ParseExecution(domain.HookSessionStart, tc.policy, tc.exit, []byte(tc.input), domain.HookRewriteContext{})
		if got.Code != tc.code || got.Feedback.Content() != tc.context || !got.Replacement.Empty() {
			t.Fatalf("incorrect SessionStart output for code %s: %+v", tc.code, got)
		}
	}
}
