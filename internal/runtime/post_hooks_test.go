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
	r.resultBudget = tool.NewResultBudget()
	if err := r.resultBudget.BeginSample(1); err != nil {
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
func TestPostHooksPersistSuccessFirstAndFreezeExplicitFeedback(t *testing.T) {
	j := &fakeJournal{}
	r, inv, runner, preparer, executor := postRuntimeFixture(t, j, domain.HookFeedbackModel, domain.HookFeedbackModel)
	runner.run = func(_ context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, input domain.HookInput, _ time.Duration) domain.HookExecution {
		if executor.calls != 1 || preparer.calls != 1 {
			t.Fatal("post ran before original effect")
		}
		results := 0
		for _, batch := range j.snapshot() {
			for _, d := range batch {
				if d.EventKind() == session.EventToolCallResult {
					results++
				}
			}
		}
		if results != 1 {
			t.Fatal("post ran without unique durable result")
		}
		b, err := input.Bytes()
		if err != nil || input.Event != domain.HookPostToolUse || input.Response == nil || strings.Contains(string(b), "artifact_id") {
			t.Fatal("unsafe post projection", err)
		}
		if id.Ordinal == 0 {
			return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookError, Code: domain.HookExitFailed, ExitKnown: true, ExitCode: 2, CleanupConfirmed: true, Origin: "execution"}}
		}
		feedback, _ := domain.NewHookFeedbackCandidate("explicit-model-feedback")
		return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}, Feedback: feedback}
	}
	emit := postApprovalEmitter(t, r, inv, protocol.HookTrustSession, tool.ApprovalAllowOnce)
	result, _, err := r.executeApproved(context.Background(), emit, runtimeTurnID, inv)
	if err != nil || result.Status() != tool.ResultSuccess || result.Code() != "ok" || runner.calls != 2 {
		t.Fatal("post failure changed tool or stopped sibling check", err, result.Status(), runner.calls)
	}
	state := r.hooks.post[inv.InvocationID()]
	if state.terminal == nil || state.terminal.Code != domain.HookExitFailed || state.terminal.Disposition != domain.PostHookCompleted || r.resultTicket != nil {
		t.Fatal("missing completed ticket")
	}
	text, err := tool.RenderPostFeedback(postFeedback(state))
	if err != nil || !strings.Contains(text, "explicit-model-feedback") || !strings.Contains(text, "hook_exit_failed") || r.resultBudget.SampleUsed() != result.Budget().ActualBytes()+len(text) {
		t.Fatal("wrong frozen feedback charge", err)
	}
	before := len(j.snapshot())
	again, _, err := r.executeApproved(context.Background(), emit, runtimeTurnID, inv)
	if err != nil || again.Preview().Text() != result.Preview().Text() || len(j.snapshot()) != before || runner.calls != 2 || executor.calls != 1 {
		t.Fatal("replay reran side effects", err)
	}
}
func TestPostTrustAndApprovalBothPrecedeEffects(t *testing.T) {
	for _, mode := range []string{"headless", "deny-trust", "deny-approval"} {
		t.Run(mode, func(t *testing.T) {
			j := &fakeJournal{}
			r, inv, runner, preparer, executor := postRuntimeFixture(t, j, domain.HookFeedbackModel)
			trust, approval := protocol.HookTrustSession, tool.ApprovalAllowOnce
			if mode == "headless" {
				r.interactiveApprovals = false
			}
			if mode == "deny-trust" {
				trust = protocol.HookTrustDeny
			}
			if mode == "deny-approval" {
				approval = tool.ApprovalDeny
			}
			emit := postApprovalEmitter(t, r, inv, trust, approval)
			wrapped := func(e protocol.Event) {
				if e.Kind == protocol.EventHookTrustRequested && (preparer.calls != 0 || executor.calls != 0) {
					t.Fatal("trust after candidate")
				}
				emit(e)
			}
			result, _, err := r.executeApproved(context.Background(), wrapped, runtimeTurnID, inv)
			if err != nil || result.Status() != tool.ResultDenied || runner.calls != 0 || executor.calls != 0 || r.hooks.post[inv.InvocationID()].terminal == nil || r.resultTicket != nil {
				t.Fatal("denial failed closed", err)
			}
			if mode != "deny-approval" && preparer.calls != 0 {
				t.Fatal("trust denial reached candidate")
			}
			if len(postFeedback(r.hooks.post[inv.InvocationID()])) != 0 {
				t.Fatal("unsuccessful tool disclosed feedback")
			}
		})
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

func TestPreAndPostShareExecutionBudgetAndKeepFrozenPrefix(t *testing.T) {
	testPrePostSharedExecutionBudget(t, false)
}
func TestPreAndFailureShareExecutionBudgetAndKeepFrozenPrefix(t *testing.T) {
	testPrePostSharedExecutionBudget(t, true)
}
func testPrePostSharedExecutionBudget(t *testing.T, failed bool) {
	j := &fakeJournal{}
	r, inv, runner, _, executor := postRuntimeFixture(t, j, domain.HookFeedbackModel, domain.HookFeedbackModel)
	declarations := r.hooks.snapshot.Declarations()
	event, wantStatus := domain.HookPostToolUse, tool.ResultSuccess
	if failed {
		event = domain.HookPostToolUseFailure
		wantStatus = tool.ResultError
		executor.outcome = wantStatus
	}
	pre, err := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPreToolUse, domain.HookFeedbackNone, strings.Repeat("a", 64), "pre", 7, "true", 30)
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range declarations {
		declarations[i], err = domain.NewHookDeclaration(d.Reference.Source, event, d.Reference.Feedback, d.Reference.Revision, d.Reference.ID, d.Matcher, d.Command, 30)
		if err != nil {
			t.Fatal(err)
		}
	}
	declarations = append([]domain.HookDeclaration{pre}, declarations...)
	snapshot, err := domain.NewHookSnapshot(declarations, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.InstallHooks(snapshot, runner, "/workspace", domain.HookOK); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(0, 0)
	r.hooks.now = func() time.Time { return now }
	var events []domain.HookEvent
	runner.run = func(_ context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, _ domain.HookInput, timeout time.Duration) domain.HookExecution {
		events = append(events, id.Event)
		feedback := domain.HookFeedbackCandidate{}
		if id.Event == domain.HookPreToolUse {
			if executor.calls != 0 || timeout != 30*time.Second {
				t.Fatal("pre admission mismatch", timeout)
			}
			now = now.Add(20 * time.Second)
		} else {
			if executor.calls != 1 || timeout != 10*time.Second || id.Ordinal != 0 {
				t.Fatal("shared execution budget/trust wait mismatch", timeout, id.Ordinal)
			}
			now = now.Add(11 * time.Second)
			feedback, _ = domain.NewHookFeedbackCandidate("prefix must remain durable")
		}
		return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}, Feedback: feedback}
	}
	base := postApprovalEmitter(t, r, inv, protocol.HookTrustSession, tool.ApprovalAllowOnce)
	result, _, err := r.executeApproved(context.Background(), func(e protocol.Event) {
		if e.Kind == protocol.EventHookTrustRequested {
			p, _ := protocol.DecodeHookTrustRequested(e)
			if p.Declaration.Reference.Event.Post() {
				now = now.Add(100 * time.Second)
			}
		}
		base(e)
	}, runtimeTurnID, inv)
	if err != nil || result.Status() != wantStatus || len(events) != 2 || events[0] != domain.HookPreToolUse || events[1] != event {
		t.Fatal("combined phase execution failed", err, events)
	}
	state := r.hooks.post[inv.InvocationID()]
	if state.terminal == nil || state.terminal.Disposition != domain.PostHookSkipped || state.terminal.Code != domain.HookExecutionBudgetExhausted || state.terminal.Processed != 1 || len(state.terminal.SuffixFeedback) != 1 || len(state.finished) != 1 || state.finished[0].Result.TimedOut {
		t.Fatal("fabricated unstarted suffix execution")
	}
	text, err := tool.RenderPostFeedback(postFeedback(state))
	if err != nil || !strings.Contains(text, "prefix must remain durable") || !strings.Contains(text, "hook_execution_budget_exhausted") {
		t.Fatal("durable prefix evicted for suffix", err)
	}
	if r.resultBudget.SampleUsed() != result.Budget().ActualBytes()+len(text) || r.resultTicket != nil {
		t.Fatal("ticket closed or charged incorrectly")
	}
}

func TestPostCancellationAfterEachAcceptedFactKeepsDurableOutcome(t *testing.T) {
	for _, kind := range []session.EventKind{session.EventToolPostHookPlanned, session.EventToolCallResult, session.EventHookExecutionStarted, session.EventHookExecutionFinished, session.EventToolPostHookCompleted} {
		t.Run(string(kind), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			j := &hookFaultJournal{kind: kind, after: true, cancel: cancel}
			r, inv, runner, _, executor := postRuntimeFixture(t, j, domain.HookFeedbackModel)
			runner.run = func(ctx context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, _ domain.HookInput, _ time.Duration) domain.HookExecution {
				result := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
				if ctx.Err() != nil {
					result.Status, result.Code, result.Cancelled = domain.HookCancel, domain.HookCancelled, true
				}
				return domain.HookExecution{Result: result}
			}
			result, _, _ := r.executeApproved(ctx, postApprovalEmitter(t, r, inv, protocol.HookTrustSession, tool.ApprovalAllowOnce), runtimeTurnID, inv)
			if result.Validate() != nil || r.poisoned.Load() || r.resultTicket != nil {
				t.Fatal("accepted cancellation lost definite result or ownership")
			}
			state := r.hooks.post[inv.InvocationID()]
			if state == nil || state.terminal == nil {
				t.Fatal("cancellation left post barrier open")
			}
			if kind == session.EventToolPostHookPlanned {
				if executor.calls != 0 || runner.calls != 0 || result.Status() != tool.ResultCancelled || state.terminal.Disposition != domain.PostHookSkipped {
					t.Fatal("plan cancellation ran tool/check")
				}
			} else if result.Status() != tool.ResultSuccess || executor.calls != 1 {
				t.Fatal("cancellation rewrote successful effect")
			}
			if kind == session.EventToolCallResult && (runner.calls != 0 || state.terminal.Processed != 0 || state.terminal.Disposition != domain.PostHookCancelled) {
				t.Fatal("result cancellation fabricated post start")
			}
			if kind == session.EventHookExecutionStarted && (runner.calls != 1 || state.terminal.Processed != 1 || state.terminal.Disposition != domain.PostHookCancelled) {
				t.Fatal("accepted start not resolved exactly once")
			}
			if (kind == session.EventHookExecutionFinished || kind == session.EventToolPostHookCompleted) && (state.terminal.Disposition != domain.PostHookCompleted || state.terminal.Code != domain.HookOK) {
				t.Fatal("accepted terminal rewritten by later cancellation")
			}
		})
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
	r.hooks.completed[inv.InvocationID()] = tool.NewInvocationErrorResult(inv, tool.ResultError, "forged", "forged cached result")
	if _, _, err := r.executeApproved(context.Background(), emit, runtimeTurnID, inv); err == nil {
		t.Fatal("forged cached result accepted")
	}
	r.hooks.completed[inv.InvocationID()] = result
	if runner.calls != 1 || executor.calls != 1 || len(j.snapshot()) != before {
		t.Fatal("cache mismatch produced new side effects/facts")
	}
}
func TestPostForgedRunnerIdentityBecomesUncertainWithoutRewritingSuccess(t *testing.T) {
	j := &fakeJournal{}
	r, inv, runner, _, _ := postRuntimeFixture(t, j, domain.HookFeedbackModel)
	runner.run = func(_ context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, _ domain.HookInput, _ time.Duration) domain.HookExecution {
		id.PlanDigest = strings.Repeat("f", 64)
		return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}}
	}
	result, _, err := r.executeApproved(context.Background(), postApprovalEmitter(t, r, inv, protocol.HookTrustSession, tool.ApprovalAllowOnce), runtimeTurnID, inv)
	if err == nil || !r.poisoned.Load() || result.Status() != tool.ResultSuccess || r.hooks.post[inv.InvocationID()].terminal.Disposition != domain.PostHookUncertain || runner.calls != 1 {
		t.Fatal("forged runner result did not fail closed", err)
	}
}
