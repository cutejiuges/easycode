package builtin

import (
	"easycode/internal/domain"
	"easycode/internal/executor/process"
	"errors"
	"strings"
	"testing"
)

func TestHookClassificationPriorityAndEventExitTwo(t *testing.T) {
	for _, event := range []domain.HookEvent{domain.HookPreToolUse, domain.HookPostToolUse} {
		d, err := domain.NewHookDeclaration(domain.HookSourceProject, event, domain.HookFeedbackNone, strings.Repeat("a", 64), "check", 7, "true", 5)
		if err != nil {
			t.Fatal(err)
		}
		plan, _ := domain.NewHookPlan("018f1d8a-7b5c-7def-8abc-456789abcdef", domain.HookExec, []domain.HookReference{d.Reference})
		id, _ := domain.NewHookRunIdentity(plan, 0)
		for _, test := range []struct {
			name   string
			owned  process.Result
			output int64
			code   domain.HookCode
			parsed bool
		}{
			{"uncertain", process.Result{Uncertain: true, Cancelled: true, TimedOut: true, StdoutError: errors.New("I/O")}, 17000, domain.HookOutcomeUncertain, false},
			{"cancel", process.Result{Cancelled: true, TimedOut: true, StdoutError: errors.New("I/O")}, 17000, domain.HookCancelled, false},
			{"timeout", process.Result{TimedOut: true, StdoutError: errors.New("I/O")}, 17000, domain.HookTimedOut, false},
			{"limit", process.Result{StdoutError: errors.New("I/O"), ExitKnown: true, ExitCode: 2}, 17000, domain.HookOutputLimit, false},
			{"I/O", process.Result{StdoutError: errors.New("I/O"), ExitKnown: true, ExitCode: 2}, 0, domain.HookIOFailed, false},
			{"exit-two", process.Result{ExitKnown: true, ExitCode: 2}, 0, domain.HookExitFailed, true},
		} {
			t.Run(string(event)+"/"+test.name, func(t *testing.T) {
				parsed := false
				parse := func(e domain.HookEvent, p domain.HookFeedbackPolicy, exit int, _ []byte) domain.HookParsedOutput {
					parsed = true
					if e != event || p != domain.HookFeedbackNone || exit != 2 {
						t.Fatal("parser identity changed")
					}
					if e == domain.HookPreToolUse {
						return domain.HookParsedOutput{Status: domain.HookBlock, Code: domain.HookDenied}
					}
					return domain.HookParsedOutput{Status: domain.HookError, Code: domain.HookExitFailed}
				}
				execution := classifyHookExecution(id, domain.HookFeedbackNone, test.owned, []byte("secret-sentinel"), test.output, 0, parse)
				want := test.code
				if event == domain.HookPreToolUse && test.parsed {
					want = domain.HookDenied
				}
				if execution.Result.Validate() != nil || execution.Result.Code != want || parsed != test.parsed || !execution.Feedback.Empty() {
					t.Fatal("wrong fault classification", execution.Result, parsed)
				}
			})
		}
	}
}
