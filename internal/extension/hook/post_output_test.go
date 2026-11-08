package hook

import (
	"easycode/internal/domain"
	"strings"
	"testing"
)

func TestPostOutputClosedGrammar(t *testing.T) {
	for _, text := range []string{"", " \n", "{}", `{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"check found an issue"}}`} {
		r := ParseExecution(domain.HookPostToolUse, domain.HookFeedbackModel, 0, []byte(text))
		if r.Status != domain.HookContinue || r.Code != domain.HookOK {
			t.Fatalf("valid output rejected: %q: %+v", text, r)
		}
		if strings.Contains(text, "additionalContext") != !r.Feedback.Empty() {
			t.Fatal("feedback lost or fabricated")
		}
	}
	for _, text := range []string{
		`null`, `[]`, `{} {}`, `{"hookSpecificOutput":null}`,
		`{"hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":"x"}}`,
		`{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":""}}`,
		`{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":null}}`,
		`{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"x","additionalContext":"y"}}`,
		`{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"\u0000"}}`,
		`{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"x","permissionDecision":"deny"}}`,
		`{"decision":"block"}`, `{"continue":true}`, `{"async":true}`, `{"updatedInput":{}}`, `{"updatedMCPToolOutput":{}}`,
		`{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"` + strings.Repeat("a", 4097) + `"}}`,
		string([]byte{0xff}),
	} {
		r := ParseExecution(domain.HookPostToolUse, domain.HookFeedbackModel, 0, []byte(text))
		if r.Code != domain.HookOutputInvalid || !r.Feedback.Empty() {
			t.Fatalf("invalid output accepted: %q: %+v", text, r)
		}
	}
	text := []byte(`{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"secret-sentinel"}}`)
	for _, exit := range []int{1, 2, 3, 255} {
		r := ParseExecution(domain.HookPostToolUse, domain.HookFeedbackModel, exit, text)
		if r.Status != domain.HookError || r.Code != domain.HookExitFailed || !r.Feedback.Empty() {
			t.Fatal("post nonzero must not deny/disclose", exit, r)
		}
	}
	r := ParseExecution(domain.HookPostToolUse, domain.HookFeedbackNone, 0, text)
	if r.Code != domain.HookFeedbackNotAllowed || !r.Feedback.Empty() {
		t.Fatal("none disclosed body")
	}
	r = ParseExecution(domain.HookPreToolUse, domain.HookFeedbackNone, 2, text)
	if r.Code != domain.HookDenied || !r.Feedback.Empty() {
		t.Fatal("pre semantics changed")
	}
}
