package builtin

import (
	"testing"

	"easycode/internal/domain"
	"easycode/internal/executor/process"
)

func TestHookOwnerPassesTypedReplacementAndRejectsParserMismatch(t *testing.T) {
	declaration, _ := domain.NewHookDeclaration(domain.HookSourceUser, domain.HookPreToolUse, domain.HookFeedbackNone, domain.HookDigest(nil), "check", 7, "true", 5)
	plan, _ := domain.NewHookPlan("018f1d8a-7b5c-7def-8123-456789abcdef", domain.HookWrite, []domain.HookReference{declaration.Reference})
	id, _ := domain.NewHookRunIdentity(plan, 0)
	before, _ := domain.NewHookWriteInput("original.txt", "original")
	context, _ := domain.NewHookRewriteContext(before, domain.HookWriteProfile)
	after, _ := domain.NewHookWriteInput("rewritten.txt", "")
	replacement, _ := domain.NewHookInputReplacement(after, domain.HookWriteProfile)
	command, _ := domain.NewHookExecInput("wrong", "", 1000)
	wrong, _ := domain.NewHookInputReplacement(command, domain.HookExecProfile)
	for _, mode := range []string{"replace", "invalid-output", "wrong-profile", "cancelled", "uncertain", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			called := 0
			parse := func(event domain.HookEvent, policy domain.HookFeedbackPolicy, exit int, _ []byte, c domain.HookRewriteContext) domain.HookParsedOutput {
				called++
				if event != domain.HookPreToolUse || policy != domain.HookFeedbackNone || exit != 0 || c.Profile() != domain.HookWriteProfile || !c.Input().Equal(before) {
					t.Fatal("parser context changed")
				}
				if mode == "invalid-output" {
					return domain.HookParsedOutput{Status: domain.HookError, Code: domain.HookOutputInvalid}
				}
				candidate := replacement
				if mode == "wrong-profile" {
					candidate = wrong
				}
				return domain.HookParsedOutput{Status: domain.HookContinue, Code: domain.HookOK, Replacement: candidate}
			}
			owned := process.Result{ExitKnown: true}
			observed := int64(30)
			switch mode {
			case "cancelled":
				owned.Cancelled = true
			case "uncertain":
				owned.Uncertain = true
			case "overflow":
				observed = domain.MaxHookOutputBytes + 1
			}
			execution := classifyHookExecution(id, domain.HookFeedbackNone, owned, []byte("private raw output"), observed, 0, parse, context)
			expected := domain.HookOK
			switch mode {
			case "invalid-output":
				expected = domain.HookOutputInvalid
			case "wrong-profile", "uncertain":
				expected = domain.HookOutcomeUncertain
			case "cancelled":
				expected = domain.HookCancelled
			case "overflow":
				expected = domain.HookOutputLimit
			}
			if execution.Result.Code != expected || (mode == "replace") == execution.Replacement.Empty() {
				t.Fatal("classification or candidate mismatch", execution.Result.Code)
			}
			if mode == "replace" && !execution.Replacement.Input().Equal(after) {
				t.Fatal("replacement lost")
			}
			if (mode == "cancelled" || mode == "uncertain" || mode == "overflow") && called != 0 {
				t.Fatal("higher priority failure parsed stdout")
			}
		})
	}
}
