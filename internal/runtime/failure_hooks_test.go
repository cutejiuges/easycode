package runtime

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func installFailureBranches(t *testing.T, r *Runtime, runner *postTestRunner, both bool) {
	t.Helper()
	ds := r.hooks.snapshot.Declarations()
	failure, err := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPostToolUseFailure, domain.HookFeedbackModel, strings.Repeat("a", 64), "failure", 7, "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !both {
		ds = nil
	}
	ds = append(ds, failure)
	snapshot, err := domain.NewHookSnapshot(ds, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.InstallHooks(snapshot, runner, "/workspace", domain.HookOK); err != nil {
		t.Fatal(err)
	}
}

func TestFailureCancellationAfterResultKeepsRouteAndStartsNoScript(t *testing.T) {
	for _, matched := range []bool{false, true} {
		t.Run(map[bool]string{false: "unmatched", true: "matched"}[matched], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			j := &hookFaultJournal{kind: session.EventToolCallResult, after: true, cancel: cancel}
			r, inv, runner, _, _ := postRuntimeFixture(t, j, domain.HookFeedbackModel)
			if matched {
				installFailureBranches(t, r, runner, true)
			}
			r.tools.Patch = postPatchOutcome{tool.ResultError}
			result, _, err := r.executeApproved(ctx, postApprovalEmitter(t, r, inv, protocol.HookTrustSession, tool.ApprovalAllowOnce), runtimeTurnID, inv)
			state := r.hooks.post[inv.InvocationID()]
			if result.Status() != tool.ResultError || state.route != domain.PostHookRouteFailure || runner.calls != 0 || state.terminal == nil || r.toolTurn.resultTicket != nil {
				t.Fatal("cancellation changed route or started script", err)
			}
			want := domain.HookEventUnmatched
			if matched {
				want = domain.HookCancelled
			}
			if state.terminal.Code != want || state.terminal.Processed != 0 || !matched && len(state.terminal.SuffixFeedback) != 0 {
				t.Fatal("wrong cancelled/unmatched terminal")
			}
		})
	}
}
