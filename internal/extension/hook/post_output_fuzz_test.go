package hook

import (
	"strings"
	"testing"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func FuzzPostHookOutputDisclosureBoundary(f *testing.F) {
	for _, body := range []string{
		"", "{}", "null", "{} {}", `{"hookSpecificOutput":null}`,
		`{"hookSpecificOutput":{"hookEventName":"PostToolUseFailure","additionalContext":"authorized diagnostic"}}`,
		`{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"success diagnostic"}}`,
		`{"hookSpecificOutput":{"hookEventName":"PostToolUseFailure","additionalContext":"first","additionalContext":"REJECTED-SECRET"}}`,
		`{"hookSpecificOutput":{"hookEventName":"PostToolUseFailure","additionalContext":"\u0000"}}`,
		strings.Repeat("x", domain.MaxHookOutputBytes+1),
	} {
		for _, exit := range []int{0, 2, 255} {
			f.Add([]byte(body), exit)
		}
	}
	f.Fuzz(func(t *testing.T, body []byte, exit int) {
		if len(body) > 2*domain.MaxHookOutputBytes {
			return
		}
		for _, event := range []domain.HookEvent{domain.HookPostToolUse, domain.HookPostToolUseFailure} {
			for _, policy := range []domain.HookFeedbackPolicy{domain.HookFeedbackNone, domain.HookFeedbackModel} {
				result := ParseExecution(event, policy, exit, body)
				if !result.Code.Valid() {
					t.Fatal("unclassified output exposed a free-form error")
				}
				if policy == domain.HookFeedbackNone && !result.Feedback.Empty() {
					t.Fatal("untrusted output was disclosed")
				}
				if (exit != 0 || result.Code != domain.HookOK || result.Status != domain.HookContinue) && !result.Feedback.Empty() {
					t.Fatal("rejected output retained a candidate")
				}
				if len(body) > domain.MaxHookOutputBytes && result.Code != domain.HookOutputLimit {
					t.Fatal("over-limit bytes reached output parsing")
				}
				if len(body) <= domain.MaxHookOutputBytes && exit != 0 && result.Code != domain.HookExitFailed {
					t.Fatal("post exit code acquired pre-hook denial semantics")
				}
				if result.Feedback.Empty() {
					continue
				}
				content := result.Feedback.Content()
				if len(content) > domain.MaxHookFeedbackBytes || !utf8.ValidString(content) || strings.ContainsRune(content, 0) || strings.TrimSpace(content) == "" {
					t.Fatal("invalid complete content was disclosed")
				}
				// 有反馈时只能来自严格事件匹配的唯一字段，不能包含其他捕获字节。
				var accepted struct {
					Specific struct {
						Event   domain.HookEvent `json:"hookEventName"`
						Content string           `json:"additionalContext"`
					} `json:"hookSpecificOutput"`
				}
				if codec.UnmarshalRecord(body, &accepted) != nil || accepted.Specific.Event != event || accepted.Specific.Content != content {
					t.Fatal("feedback lost its exact wire provenance")
				}
			}
		}
	})
}
