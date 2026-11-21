package builtin

import (
	"testing"

	"easycode/internal/domain"
	"easycode/internal/executor/process"
	"easycode/internal/extension/hook"
)

func TestPostCompactProcessClassifiesOnlyExitAndOwnedFaults(t *testing.T) {
	target, err := domain.NewCompactionHookTarget("018f1d8a-7b5c-7def-8abc-456789abcdef", domain.ManualCompactionScope())
	if err != nil {
		t.Fatal(err)
	}
	id, err := domain.NewPostCompactHookRunIdentity(target, domain.HookDigest(nil), 0)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("{\"async\":true}\n{\"continue\":false,\"decision\":\"block\",\"reason\":\"private\"}")
	for _, tc := range []struct {
		name        string
		owned       process.Result
		stderrBytes int64
		code        domain.HookCode
	}{
		{"inert output", process.Result{ExitKnown: true}, 0, domain.HookOK},
		{"exit two", process.Result{ExitKnown: true, ExitCode: 2}, 0, domain.HookExitFailed},
		{"overflow before exit", process.Result{ExitKnown: true, ExitCode: 2}, domain.MaxHookOutputBytes * 2, domain.HookOutputLimit},
		{"timeout before overflow", process.Result{TimedOut: true}, domain.MaxHookOutputBytes * 2, domain.HookTimedOut},
		{"cancellation before timeout", process.Result{Cancelled: true, TimedOut: true}, 0, domain.HookCancelled},
		{"uncertainty before cancellation", process.Result{Uncertain: true, Cancelled: true}, 0, domain.HookOutcomeUncertain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			execution := classifyHookExecution(id, domain.HookFeedbackNone, tc.owned, body, int64(len(body)), tc.stderrBytes, hook.ParseExecution, domain.HookRewriteContext{})
			result := execution.Result
			if result.Validate() != nil || result.Code != tc.code || result.StderrBytes != min(tc.stderrBytes, domain.PostCompactOverflowBytes) ||
				!execution.Feedback.Empty() || !execution.Replacement.Empty() {
				t.Fatalf("incorrect post-check result: %+v", execution)
			}
		})
	}
	passed := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
	for _, forged := range []domain.HookRunResult{
		{Identity: id, Status: domain.HookBlock, Code: domain.HookDenied, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"},
		{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, StdoutBytes: domain.PostCompactOverflowBytes, CleanupConfirmed: true, Origin: "execution"},
		{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, StderrBytes: domain.PostCompactOverflowBytes, CleanupConfirmed: true, Origin: "execution"},
		{Identity: id, Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain, Origin: "recovery"},
	} {
		if forged.Validate() == nil {
			t.Fatalf("accepted a forged post-check result: %+v", forged)
		}
	}
	feedback, err := domain.NewHookFeedbackCandidate("secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.NewHookExecution(passed, domain.HookFeedbackNone, feedback, domain.HookInputReplacement{}); err == nil {
		t.Fatal("accepted post-check feedback")
	}
}
