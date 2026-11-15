package hook

import (
	"strings"
	"testing"

	"easycode/internal/domain"
)

func parserRewriteContext(t *testing.T) domain.HookRewriteContext {
	t.Helper()
	input, _ := domain.NewHookWriteInput("original.txt", "original")
	context, err := domain.NewHookRewriteContext(input, domain.HookWriteProfile)
	if err != nil {
		t.Fatal(err)
	}
	return context
}
func TestPreOutputHasOneCompleteStandaloneReplacement(t *testing.T) {
	context := parserRewriteContext(t)
	valid := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"file_path":"rewritten.txt","content":""}}}`
	parsed := ParseExecution(domain.HookPreToolUse, domain.HookFeedbackNone, 0, []byte(valid), context)
	if parsed.Status != domain.HookContinue || parsed.Code != domain.HookOK || parsed.Replacement.Empty() || !parsed.Feedback.Empty() {
		t.Fatal("replacement lost", parsed.Code)
	}
	file, content, ok := parsed.Replacement.Input().Write()
	if !ok || file != "rewritten.txt" || content != "" {
		t.Fatal("replacement altered")
	}
	for _, bad := range []string{
		strings.Replace(valid, `"updatedInput":`, `"permissionDecision":"allow","updatedInput":`, 1),
		strings.Replace(valid, `"updatedInput":`, `"permissionDecision":"deny","permissionDecisionReason":"private","updatedInput":`, 1),
		strings.Replace(valid, `"updatedInput":`, `"additionalContext":"private","updatedInput":`, 1),
		strings.Replace(valid, `"content":""`, `"content":"","unknown":false`, 1),
		strings.Replace(valid, `"content":""`, `"content":null`, 1),
		strings.Replace(valid, `"content":""`, `"content":"","content":"wrong"`, 1),
		strings.Replace(valid, `,"content":""`, "", 1),
		strings.Replace(valid, `"hookSpecificOutput":`, `"async":true,"hookSpecificOutput":`, 1),
		valid + `{}`,
	} {
		r := ParseExecution(domain.HookPreToolUse, domain.HookFeedbackNone, 0, []byte(bad), context)
		if r.Code != domain.HookOutputInvalid || !r.Replacement.Empty() || !r.Feedback.Empty() {
			t.Fatalf("accepted mixed output %.90q: %s", bad, r.Code)
		}
	}
	for _, event := range []domain.HookEvent{domain.HookPostToolUse, domain.HookPostToolUseFailure, domain.HookStop} {
		body := strings.Replace(valid, "PreToolUse", string(event), 1)
		r := ParseExecution(event, domain.HookFeedbackNone, 0, []byte(body), domain.HookRewriteContext{})
		if r.Code != domain.HookOutputInvalid || !r.Replacement.Empty() {
			t.Fatal("replacement crossed event", event)
		}
	}
	for _, exit := range []int{1, 2} {
		r := ParseExecution(domain.HookPreToolUse, domain.HookFeedbackNone, exit, []byte(valid), context)
		if !r.Replacement.Empty() || exit == 1 && r.Code != domain.HookExitFailed || exit == 2 && r.Code != domain.HookDenied {
			t.Fatal("exit precedence changed")
		}
	}
}
func TestPreOutputCanonicalExpansionIsNotRawOutputOverflow(t *testing.T) {
	body := []byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"file_path":"sample.txt","content":"` + strings.Repeat("<", 12000) + `"}}}`)
	if len(body) != 12108 {
		t.Fatal("probe raw bytes changed", len(body))
	}
	r := ParseExecution(domain.HookPreToolUse, domain.HookFeedbackNone, 0, body, parserRewriteContext(t))
	if r.Code != domain.HookOutputInvalid || !r.Replacement.Empty() {
		t.Fatal("canonical overflow accepted", r.Code)
	}
}
func FuzzPreInputReplacement(f *testing.F) {
	f.Add([]byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"file_path":"f","content":""}}}`))
	f.Add([]byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":null}}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > 2*domain.MaxHookOutputBytes {
			return
		}
		r := ParseExecution(domain.HookPreToolUse, domain.HookFeedbackNone, 0, body, parserRewriteContext(t))
		if !r.Status.ValidCode(r.Code) || r.Replacement.Validate() != nil || !r.Feedback.Empty() {
			t.Fatal("invalid typed parser output")
		}
		if len(body) > domain.MaxHookOutputBytes && r.Code != domain.HookOutputLimit {
			t.Fatal("raw limit bypassed")
		}
	})
}
