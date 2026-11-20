package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
	"errors"
	"strings"
	"testing"
	"time"
)

type postTestRunner struct {
	calls int
	run   func(context.Context, domain.HookRunIdentity, domain.HookDeclaration, domain.HookInput, time.Duration) domain.HookExecution
}

func (*postTestRunner) Preflight(context.Context) domain.HookCode { return domain.HookOK }
func (f *postTestRunner) Run(ctx context.Context, id domain.HookRunIdentity, d domain.HookDeclaration, input domain.HookInput, timeout time.Duration) domain.HookExecution {
	f.calls++
	if f.run != nil {
		return f.run(ctx, id, d, input, timeout)
	}
	return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}}
}
func postRuntimeFixture(t *testing.T, journal Journal, policies ...domain.HookFeedbackPolicy) (*Runtime, tool.Invocation, *postTestRunner, *fixedPatchPreparer, *fixedPatchExecutor) {
	t.Helper()
	r, inv, _, _ := hookRuntimeFixture(t, journal, 0)
	ds := make([]domain.HookDeclaration, len(policies))
	for i, policy := range policies {
		d, err := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPostToolUse, policy, strings.Repeat("a", 64), "post-"+string(rune('a'+i)), 7, "true", 5)
		if err != nil {
			t.Fatal(err)
		}
		ds[i] = d
	}
	snapshot, err := domain.NewHookSnapshot(ds, nil)
	if err != nil {
		t.Fatal(err)
	}
	runner := &postTestRunner{}
	if err := r.InstallHooks(snapshot, runner, "/workspace", domain.HookOK); err != nil {
		t.Fatal(err)
	}
	_, candidate := runtimePatchInvocation(t)
	preparer := &fixedPatchPreparer{candidate: candidate}
	executor := &fixedPatchExecutor{}
	r.patchPreparer, r.patchExecutor = preparer, executor
	r.toolTurn.resultBudget = tool.NewResultBudget()
	if err := r.toolTurn.resultBudget.BeginSample(1); err != nil {
		t.Fatal(err)
	}
	return r, inv, runner, preparer, executor
}
func postApprovalEmitter(t *testing.T, r *Runtime, inv tool.Invocation, trust protocol.HookTrustDecision, approval tool.ApprovalDecision) Emitter {
	t.Helper()
	trusted := hookTrustEmitter(t, r, trust, nil)
	return func(e protocol.Event) {
		trusted(e)
		if e.Kind == protocol.EventToolApprovalRequested {
			p, err := protocol.DecodeToolApprovalRequested(e)
			if err != nil {
				t.Fatal(err)
			}
			id, _ := tool.ParseApprovalRequestID(p.RequestID)
			response, err := tool.NewApprovalResponse(id, runtimeTurnID, inv.InvocationID(), tool.CandidateFingerprint(p.Fingerprint), approval, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := r.ResolveToolApproval(response); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestPostEverySyncFailurePreservesRealResultAndStops(t *testing.T) {
	for _, kind := range []session.EventKind{session.EventToolPostHookPlanned, session.EventToolCallResult, session.EventHookExecutionStarted, session.EventHookExecutionFinished, session.EventToolPostHookCompleted} {
		for _, after := range []bool{false, true} {
			t.Run(string(kind)+map[bool]string{false: "/before", true: "/after"}[after], func(t *testing.T) {
				j := &hookFaultJournal{kind: kind, after: after, failure: errors.New("unconfirmed Sync")}
				r, inv, runner, _, executor := postRuntimeFixture(t, j, domain.HookFeedbackModel)
				_, _, err := r.executeApproved(context.Background(), postApprovalEmitter(t, r, inv, protocol.HookTrustSession, tool.ApprovalAllowOnce), runtimeTurnID, inv)
				if err == nil || !r.poisoned.Load() {
					t.Fatal("failed Sync did not poison", err)
				}
				wantRuns := 1
				if kind == session.EventToolPostHookPlanned || kind == session.EventToolCallResult || kind == session.EventHookExecutionStarted {
					wantRuns = 0
				}
				if runner.calls != wantRuns {
					t.Fatal("crossed durable admission", runner.calls, wantRuns)
				}
				count := 0
				for _, batch := range j.snapshot() {
					for _, draft := range batch {
						if draft.EventKind() == session.EventToolCallResult {
							count++
							p, decodeErr := session.DecodeToolCallResultPayload(session.Record{EventKind: draft.EventKind(), Payload: draft.PayloadBytes()})
							if decodeErr != nil || p.Status != tool.ResultSuccess {
								t.Fatal("rewrote real result", decodeErr)
							}
						}
					}
				}
				if count > 1 || kind != session.EventToolPostHookPlanned && executor.calls != 1 {
					t.Fatal("duplicate result/effect", count, executor.calls)
				}
			})
		}
	}
}

func TestPostCachedFactsRejectChangedInputAndForgedResult(t *testing.T) {
	j := &fakeJournal{}
	r, inv, runner, _, executor := postRuntimeFixture(t, j, domain.HookFeedbackNone)
	emit := postApprovalEmitter(t, r, inv, protocol.HookTrustSession, tool.ApprovalAllowOnce)
	result, _, err := r.executeApproved(context.Background(), emit, runtimeTurnID, inv)
	if err != nil {
		t.Fatal(err)
	}
	before := len(j.snapshot())
	replacement, _ := tool.NewPatchReplacement("old", "forged", false)
	input, _ := tool.NewPatchInput("main.go", []tool.PatchReplacement{replacement})
	ready, _ := tool.NewPatchReadyCall(inv.ProviderCallID(), inv.WireKind(), input)
	changed, _ := tool.NewInvocation(inv.InvocationID(), ready)
	if _, _, err := r.executeApproved(context.Background(), emit, runtimeTurnID, changed); err == nil {
		t.Fatal("same identity with changed input reused result")
	}
	r.toolTurn.toolCalls[inv.InvocationID()].result = tool.NewInvocationErrorResult(inv, tool.ResultError, "forged", "forged cached result")
	if _, _, err := r.executeApproved(context.Background(), emit, runtimeTurnID, inv); err == nil {
		t.Fatal("forged cached result accepted")
	}
	r.toolTurn.toolCalls[inv.InvocationID()].result = result
	if runner.calls != 1 || executor.calls != 1 || len(j.snapshot()) != before {
		t.Fatal("cache mismatch produced new side effects/facts")
	}
}
