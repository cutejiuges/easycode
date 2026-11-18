package hook

import (
	"testing"

	"easycode/internal/domain"
)

func TestStopOutputHasOneExplicitModelDisclosurePath(t *testing.T) {
	for _, policy := range []domain.HookFeedbackPolicy{domain.HookFeedbackNone, domain.HookFeedbackModel} {
		for _, empty := range []string{"", " \n\t", "{}", " { } \n"} {
			r := ParseExecution(domain.HookStop, policy, 0, []byte(empty), domain.HookRewriteContext{})
			if r.Status != domain.HookContinue || r.Code != domain.HookOK || !r.Feedback.Empty() {
				t.Fatal("empty did not pass", r)
			}
		}
		r := ParseExecution(domain.HookStop, policy, 0, []byte(`{"decision":"block","reason":"  尚未满足\n "}`), domain.HookRewriteContext{})
		if r.Status != domain.HookBlock || r.Code != domain.HookDenied {
			t.Fatal("block rejected", r)
		}
		if policy == domain.HookFeedbackNone && !r.Feedback.Empty() || policy == domain.HookFeedbackModel && r.Feedback.Content() != "  尚未满足\n " {
			t.Fatal("disclosure or whitespace changed")
		}
		for _, exit := range []int{2, 3} {
			r := ParseExecution(domain.HookStop, policy, exit, []byte("raw secret sentinel"), domain.HookRewriteContext{})
			if !r.Feedback.Empty() || exit == 2 && (r.Status != domain.HookBlock || r.Code != domain.HookDenied) || exit == 3 && (r.Status != domain.HookError || r.Code != domain.HookExitFailed) {
				t.Fatal("exit disclosure", r)
			}
		}
	}
	valid := `{"decision":"block","reason":"fix"}`
	for _, event := range []domain.HookEvent{domain.HookPreToolUse, domain.HookPostToolUse, domain.HookPostToolUseFailure} {
		policy := domain.HookFeedbackNone
		if event.Post() {
			policy = domain.HookFeedbackModel
		}
		r := ParseExecution(event, policy, 0, []byte(valid), domain.HookRewriteContext{})
		if r.Code != domain.HookOutputInvalid || !r.Feedback.Empty() {
			t.Fatal("stop output leaked into tool event", event, r)
		}
	}
}
