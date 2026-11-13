package runtime

import (
	"context"
	"strings"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
)

func TestStopRequestBudgetReturnsOnlyUnadmittedSteeringAndStillDeliversFeedback(t *testing.T) {
	for _, feedbackOver := range []bool{false, true} {
		t.Run(map[bool]string{false: "user-over", true: "feedback-over"}[feedbackOver], func(t *testing.T) {
			j := &fakeJournal{}
			r, runner, requests := newStopRuntimeFixture(t, j)
			original := runner.run
			runner.run = func(ctx context.Context, id domain.HookRunIdentity, d domain.HookDeclaration) domain.HookExecution {
				if len(runner.inputs) > 1 {
					return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}}
				}
				return original(ctx, id, d)
			}
			j.hook = func(ds []session.RecordDraft) {
				for _, draft := range ds {
					if draft.EventKind() == session.EventTurnStopHookDecided {
						d, _ := domain.DecodeStopDecision(draft.PayloadBytes())
						if d.Disposition != domain.StopContinued {
							continue
						}
						plan, err := r.contextPlan()
						if err != nil {
							t.Fatal(err)
						}
						base, _ := plan.TotalEstimate().Tokens()
						extra := uint64(1000)
						if feedbackOver {
							extra = 1
						}
						r.budget, _ = contextplan.NewBudget(base+extra, 0, 0)
					}
				}
			}
			input, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000001", "task")
			correction, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000002", strings.Repeat("large correction ", 2000))
			control := &stopInputFixture{inputs: []domain.UserInput{correction}}
			trust := hookTrustEmitter(t, r, protocol.HookTrustSession, nil)
			commits := 0
			err := r.runTurn(context.Background(), []domain.UserInput{input}, control, func(e protocol.Event) {
				trust(e)
				if e.Kind == protocol.EventUserInputCommitted {
					p, _ := protocol.DecodeUserInputCommitted(e)
					if p.InputID == correction.ID() {
						commits++
					}
				}
			})
			want := int32(2)
			if feedbackOver {
				want = 1
			}
			if (err != nil) != feedbackOver || requests.Load() != want || control.claims != 1 || commits != 0 || control.states[len(control.states)-1] != protocol.InputReturned {
				t.Fatal("budget changed claim ownership or network", err, requests.Load(), control.claims, control.states)
			}
			deliveries, summaries := 0, 0
			for _, batch := range j.snapshot() {
				if len(batch) == 1 && batch[0].EventKind() == session.EventProviderNativeCommit {
					deliveries++
				}
				for _, draft := range batch {
					if draft.EventKind() == session.EventCompactionStarted {
						summaries++
					}
				}
			}
			if deliveries != 1 || summaries != 0 {
				t.Fatal("accepted feedback was dropped or summary retried", deliveries, summaries)
			}
		})
	}
}
