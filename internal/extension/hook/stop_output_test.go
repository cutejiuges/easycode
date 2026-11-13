package hook

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func TestStopOutputHasOneExplicitModelDisclosurePath(t *testing.T) {
	for _, policy := range []domain.HookFeedbackPolicy{domain.HookFeedbackNone, domain.HookFeedbackModel} {
		for _, empty := range []string{"", " \n\t", "{}", " { } \n"} {
			r := ParseExecution(domain.HookStop, policy, 0, []byte(empty))
			if r.Status != domain.HookContinue || r.Code != domain.HookOK || !r.Feedback.Empty() {
				t.Fatal("empty did not pass", r)
			}
		}
		r := ParseExecution(domain.HookStop, policy, 0, []byte(`{"decision":"block","reason":"  尚未满足\n "}`))
		if r.Status != domain.HookBlock || r.Code != domain.HookDenied {
			t.Fatal("block rejected", r)
		}
		if policy == domain.HookFeedbackNone && !r.Feedback.Empty() || policy == domain.HookFeedbackModel && r.Feedback.Content() != "  尚未满足\n " {
			t.Fatal("disclosure or whitespace changed")
		}
		for _, exit := range []int{2, 3} {
			r := ParseExecution(domain.HookStop, policy, exit, []byte("raw secret sentinel"))
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
		r := ParseExecution(event, policy, 0, []byte(valid))
		if r.Code != domain.HookOutputInvalid || !r.Feedback.Empty() {
			t.Fatal("stop output leaked into tool event", event, r)
		}
	}
}

func TestStopOutputRejectsAmbiguousUnicodeAndLimits(t *testing.T) {
	for _, bad := range []string{`null`, `[]`, `{"decision":"allow","reason":"x"}`, `{"decision":"block"}`, `{"decision":"block","reason":null}`, `{"decision":"block","reason":" "}`, `{"decision":"block","reason":"x","reason":"y"}`, `{"decision":"block","reason":"x","continue":true}`, `{"decision":"block","reason":"x"} {}`, `{"decision":"block","reason":"\ud800"}`, `{"decision":"block","reason":"\u0000"}`, `{"hookSpecificOutput":{"hookEventName":"Stop","additionalContext":"x"}}`} {
		r := ParseExecution(domain.HookStop, domain.HookFeedbackModel, 0, []byte(bad))
		if r.Code != domain.HookOutputInvalid || !r.Feedback.Empty() {
			t.Fatal("invalid output accepted", bad, r)
		}
	}
	for _, reason := range []string{strings.Repeat("x", domain.MaxHookFeedbackBytes), strings.Repeat("x", domain.MaxHookFeedbackBytes+1), strings.Repeat("\x01", domain.MaxHookFeedbackBytes)} {
		b, err := codec.MarshalStable(struct {
			Decision string `json:"decision"`
			Reason   string `json:"reason"`
		}{"block", reason})
		if err != nil {
			t.Fatal(err)
		}
		r := ParseExecution(domain.HookStop, domain.HookFeedbackModel, 0, b)
		want := domain.HookDenied
		if len(reason) > domain.MaxHookFeedbackBytes {
			want = domain.HookOutputInvalid
		}
		if len(b) > domain.MaxHookOutputBytes {
			want = domain.HookOutputLimit
		}
		if r.Code != want {
			t.Fatal("encoded limit mismatch", len(b), r.Code, want)
		}
	}
	prefix := []byte(`{"decision":"block","reason":"fix"}`)
	oversized := append(append([]byte(nil), prefix...), bytes.Repeat([]byte(" "), domain.MaxHookOutputBytes)...)
	for _, exit := range []int{0, 2} {
		r := ParseExecution(domain.HookStop, domain.HookFeedbackModel, exit, oversized)
		if r.Code != domain.HookOutputLimit || !r.Feedback.Empty() {
			t.Fatal("truncated complete prefix accepted", r)
		}
	}
	r := ParseExecution(domain.HookStop, domain.HookFeedbackModel, 0, []byte{255})
	if r.Code != domain.HookOutputInvalid {
		t.Fatal("invalid UTF-8 accepted")
	}
}

func FuzzStopOutputNeverDisclosesUnauthorizedBytes(f *testing.F) {
	for _, seed := range []string{"", `{}`, `{"decision":"block","reason":"fix"}`, `{"decision":"block","reason":"\ud800"}`} {
		f.Add([]byte(seed), 0)
	}
	f.Fuzz(func(t *testing.T, b []byte, exit int) {
		for _, policy := range []domain.HookFeedbackPolicy{domain.HookFeedbackNone, domain.HookFeedbackModel} {
			r := ParseExecution(domain.HookStop, policy, exit, b)
			if _, err := domain.NewHookParsedOutput(domain.HookStop, policy, r.Status, r.Code, r.Feedback); err != nil {
				t.Fatal("invalid typed parser result", err)
			}
			if !r.Feedback.Empty() && (policy != domain.HookFeedbackModel || exit != 0 || r.Status != domain.HookBlock || r.Code != domain.HookDenied || r.Feedback.Validate() != nil) {
				t.Fatal("unauthorized feedback")
			}
		}
	})
}
