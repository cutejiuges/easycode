package runtime

import (
	"context"
	"errors"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/session"
)

func TestHookEveryDurableBoundaryStopsAfterUnconfirmedSync(t *testing.T) {
	kinds := []session.EventKind{session.EventToolHookGatePlanned, session.EventHookExecutionStarted, session.EventHookExecutionFinished, session.EventToolHookGateDecided, session.EventToolCallResult}
	for _, kind := range kinds {
		for _, after := range []bool{false, true} {
			t.Run(string(kind)+map[bool]string{false: "/before", true: "/after"}[after], func(t *testing.T) {
				j := &hookFaultJournal{kind: kind, after: after, failure: errors.New("unconfirmed Sync")}
				r, inv, runner, _ := hookRuntimeFixture(t, j, 1)
				runner.status, runner.code = domain.HookBlock, domain.HookDenied
				_, _, err := r.checkToolHooks(context.Background(), hookTrustEmitter(t, r, "trust_session", nil), runtimeTurnID, inv)
				wantRuns := 1
				if kind == session.EventToolHookGatePlanned || kind == session.EventHookExecutionStarted {
					wantRuns = 0
				}
				if err == nil || !r.poisoned.Load() || runner.runs != wantRuns {
					t.Fatal("unconfirmed Sync crossed execution boundary", kind, after, err, runner.runs)
				}
				var starts, finishes int
				for _, batch := range j.snapshot() {
					for _, draft := range batch {
						if draft.EventKind() == session.EventHookExecutionStarted {
							starts++
						}
						if draft.EventKind() == session.EventHookExecutionFinished {
							finishes++
						}
						if draft.EventKind() == session.EventToolExecutionStarted || draft.EventKind() == session.EventToolApprovalDecided {
							t.Fatal("uncertain hook admitted target")
						}
					}
				}
				if starts > 1 || finishes > 1 || runner.runs > starts {
					t.Fatal("duplicate or non-durable execution")
				}
			})
		}
	}
}

func TestHookCancellationAfterFactAcceptancePreservesCommittedOutcome(t *testing.T) {
	for _, kind := range []session.EventKind{session.EventToolHookGatePlanned, session.EventHookExecutionStarted, session.EventHookExecutionFinished, session.EventToolHookGateDecided, session.EventToolCallResult} {
		t.Run(string(kind), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			j := &hookFaultJournal{kind: kind, after: true, cancel: cancel}
			r, inv, runner, _ := hookRuntimeFixture(t, j, 1)
			runner.status, runner.code = domain.HookBlock, domain.HookDenied
			runner.onRun = func(ctx context.Context, id domain.HookRunIdentity) domain.HookRunResult {
				if ctx.Err() != nil {
					return domain.HookRunResult{Identity: id, Status: domain.HookCancel, Code: domain.HookCancelled, Cancelled: true, CleanupConfirmed: true, Origin: "execution"}
				}
				return domain.HookRunResult{Identity: id, Status: domain.HookBlock, Code: domain.HookDenied, ExitKnown: true, ExitCode: 2, CleanupConfirmed: true, Origin: "execution"}
			}
			result, blocked, err := r.checkToolHooks(ctx, hookTrustEmitter(t, r, "trust_session", nil), runtimeTurnID, inv)
			if !blocked || result.Validate() != nil || r.poisoned.Load() {
				t.Fatal("accepted cancellation lost durable result", err)
			}
			wantRuns := 1
			if kind == session.EventToolHookGatePlanned {
				wantRuns = 0
			}
			if runner.runs != wantRuns {
				t.Fatal("accepted started was not delivered exactly once")
			}
			want := domain.HookDenied
			if kind == session.EventToolHookGatePlanned || kind == session.EventHookExecutionStarted {
				want = domain.HookCancelled
			}
			if result.Code() != string(want) {
				t.Fatal("committed outcome was rewritten", result.Code(), want)
			}
		})
	}
}
