package testsupport

import (
	"testing"

	"easycode/internal/domain"
)

const StopTurn domain.TurnID = "01900000-0000-7000-8000-000000000001"

func StopFeedback(t *testing.T, index uint32) domain.StopFeedback {
	t.Helper()
	declaration, err := domain.NewStopHookDeclaration(domain.HookSourceUser, domain.HookFeedbackModel, domain.HookDigest(nil), "completion", "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := domain.NewStopChecksPlan(domain.AssistantSampleReference{TurnID: StopTurn, SampleIndex: index}, uint8(index), []domain.HookReference{declaration.Reference})
	if err != nil {
		t.Fatal(err)
	}
	id, err := domain.NewStopHookRunIdentity(plan, 0)
	if err != nil {
		t.Fatal(err)
	}
	reason, _ := domain.NewHookFeedbackCandidate("  补齐遗漏项\n")
	feedback, err := domain.NewStopFeedback(domain.HookRunResult{Identity: id, Status: domain.HookBlock, Code: domain.HookDenied, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}, declaration.Reference, reason)
	if err != nil {
		t.Fatal(err)
	}
	return feedback
}
