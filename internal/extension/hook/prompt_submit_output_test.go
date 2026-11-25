package hook

import (
	"testing"

	"easycode/internal/domain"
)

func TestUserPromptSubmitOutputIsOnlyLocalControl(t *testing.T) {
	for _, tc := range []struct {
		exit   int
		stdout string
		want   domain.HookCode
	}{
		{0, " \n", domain.HookOK},
		{0, `{}`, domain.HookOK},
		{2, "", domain.HookDenied},
		{2, `{"continue":true}`, domain.HookDenied},
		{0, `{"decision":"block"}`, domain.HookDenied},
		{0, `{"decision":"block","reason":"private-script-reason"}`, domain.HookDenied},
		{1, `{"decision":"block"}`, domain.HookExitFailed},
		{0, `{"decision":"block","reason":null}`, domain.HookOutputInvalid},
		{0, `{"decision":"block","decision":"block"}`, domain.HookOutputInvalid},
		{0, `{"decision":"approve"}`, domain.HookOutputInvalid},
		{0, `{"continue":false,"stopReason":"private"}`, domain.HookOutputInvalid},
		{0, `{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"private"}}`, domain.HookOutputInvalid},
		{0, `{"decision":"block"}{}`, domain.HookOutputInvalid},
		{0, "private", domain.HookOutputInvalid},
		{0, "\xff", domain.HookOutputInvalid},
	} {
		parsed := ParseExecution(domain.HookUserPromptSubmit, domain.HookFeedbackNone, tc.exit, []byte(tc.stdout), domain.HookRewriteContext{})
		if parsed.Code != tc.want || !parsed.Status.ValidCode(parsed.Code) || !parsed.Feedback.Empty() || !parsed.Replacement.Empty() {
			t.Fatalf("exit %d with bounded output produced %q/%q", tc.exit, parsed.Status, parsed.Code)
		}
	}
}
