package runtime

import (
	"context"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func TestStopCancelledTrustCannotInstallLateGrant(t *testing.T) {
	r, runner, _ := newStopRuntimeFixture(t, &fakeJournal{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000001", "task")
	err := r.runFixtureTurn(ctx, []domain.UserInput{input}, func(e protocol.Event) {
		if e.Kind != protocol.EventHookTrustRequested {
			return
		}
		p, _ := protocol.DecodeHookTrustRequested(e)
		response, _ := protocol.NewHookTrustResponse(r.sessionID, r.threadID, e.TurnID, p, protocol.HookTrustSession)
		cancel()
		if r.ResolveHookTrust(response) == nil {
			t.Error("late cancelled grant installed")
		}
	})
	if err == nil || len(runner.inputs) != 0 || r.HookEntries()[0].State != "untrusted" {
		t.Fatal("cancelled trust executed or persisted")
	}
}
