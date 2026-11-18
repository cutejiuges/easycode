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
