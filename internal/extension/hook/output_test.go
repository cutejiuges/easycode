package hook

import (
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestOutputSubsetAndNoFreeTextFeedback(t *testing.T) {
	deny := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"secret-sentinel"}}`
	for _, test := range []struct {
		exit int
		out  string
		code domain.HookCode
	}{
		{0, "", domain.HookOK}, {0, " \n", domain.HookOK}, {0, "{ }", domain.HookOK}, {0, deny, domain.HookDenied},
		{2, "invalid", domain.HookDenied}, {1, deny, domain.HookExitFailed},
		{0, strings.Replace(deny, `"deny"`, `"allow"`, 1), domain.HookOutputInvalid},
		{0, strings.Replace(deny, `"deny"`, `"ask"`, 1), domain.HookOutputInvalid},
		{0, strings.Replace(deny, `"hookEventName":`, `"updatedInput":{},"hookEventName":`, 1), domain.HookOutputInvalid},
		{0, `{"decision":"block","reason":"secret"}`, domain.HookOutputInvalid},
		{0, `{"hookSpecificOutput":null}`, domain.HookOutputInvalid}, {0, "{} {}", domain.HookOutputInvalid},
		{0, string([]byte{0xff}), domain.HookOutputInvalid}, {0, strings.Repeat(" ", domain.MaxHookOutputBytes) + "{}", domain.HookOutputLimit},
	} {
		status, code := ParseOutput(test.exit, []byte(test.out))
		if code != test.code || !status.ValidCode(code) {
			t.Fatalf("exit %d output %.30q got %s/%s", test.exit, test.out, status, code)
		}
		if strings.Contains(string(code), "secret") {
			t.Fatal("secret escaped")
		}
	}
}
