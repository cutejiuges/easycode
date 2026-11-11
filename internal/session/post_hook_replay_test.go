package session

import (
	"easycode/internal/domain"
	"easycode/internal/tool"
	"strings"
	"testing"
)

func postReplayFixture(t *testing.T, policies ...domain.HookFeedbackPolicy) (*replayFixture, tool.Invocation, domain.PostHookPlan) {
	t.Helper()
	f := newReplayFixture(t)
	f.cwd = "/workspace"
	f.appendMetadata()
	f.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
	f.appendInitialInputs(testTurnID)
	inv := sessionPatchInvocation(t)
	ready, err := NewToolCallReadyDraft(testTurnID, inv, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.appendBatch(mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()), mustDraft(t, EventSampleUsage, testTurnID, testSampleUsage(t)), ready)
	refs := make([]domain.HookReference, len(policies))
	model := false
	for i, policy := range policies {
		d, err := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPostToolUse, policy, strings.Repeat("a", 64), "post-"+string(rune('a'+i)), 7, "true", 5)
		if err != nil {
			t.Fatal(err)
		}
		refs[i] = d.Reference
		model = model || policy == domain.HookFeedbackModel
	}
	plan, err := domain.NewHookPlan(string(inv.InvocationID()), domain.HookPatch, refs)
	if err != nil {
		t.Fatal(err)
	}
	reserved := 0
	delivery := domain.HookDeliveryNone
	if model {
		reserved = 16 << 10
		delivery = domain.HookDeliveryAvailable
	}
	currentPostBranch1, _ := domain.NewPostHookBranch(plan.Event, plan.Hooks, reserved)
	currentPostBranch1.Delivery = delivery
	p, err := domain.NewPostHookPlan(plan.InvocationID, plan.Capability, []domain.PostHookBranch{currentPostBranch1}, 256<<10, reserved)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := NewToolPostHookPlannedDraft(testTurnID, p)
	if err != nil {
		t.Fatal(err)
	}
	f.appendBatch(draft)
	return f, inv, p
}
func appendPostToolSuccess(t *testing.T, f *replayFixture, inv tool.Invocation, p domain.PostHookPlan) tool.InvocationResult {
	t.Helper()
	metadata, _ := NewApprovalMetadata(inv)
	request, _ := tool.ParseApprovalRequestID(string(inv.InvocationID()))
	approval, err := NewToolApprovalDecidedDraft(testTurnID, ToolApprovalDecidedPayload{RequestID: request, InvocationID: inv.InvocationID(), Fingerprint: tool.NewCandidateFingerprint([]byte("candidate")), Outcome: tool.ApprovalOutcomeAllowOnce, Source: ApprovalSourceInteractive, Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	f.appendBatch(approval)
	started, err := NewToolExecutionStartedDraft(testTurnID, inv.InvocationID())
	if err != nil {
		t.Fatal(err)
	}
	f.appendBatch(started)
	patch, _ := inv.Patch()
	m, _ := tool.NewPatchResultMetadata(patch.Input().FilePath(), 1, 1, 1)
	preview, _ := tool.NewModelPreview("patched")
	result, err := tool.NewPatchInvocationResult(patch, tool.ResultSuccess, "ok", preview, m)
	if err != nil {
		t.Fatal(err)
	}
	result, err = tool.FinalizeResult(result, p.Allowance-p.Reservation, tool.BudgetPreserved)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := NewToolCallResultDraft(testTurnID, result)
	if err != nil {
		t.Fatal(err)
	}
	f.appendBatch(draft)
	return result
}
func postFinished(t *testing.T, p domain.PostHookPlan, prior []domain.FrozenHookFeedback, status domain.HookOutcome, code domain.HookCode, body string) domain.HookFinished {
	t.Helper()
	id, _ := domain.NewPostHookRunIdentity(p, domain.HookPostToolUse, len(prior))
	exit := 0
	if code == domain.HookExitFailed {
		exit = 2
	}
	stats := domain.HookRunResult{Identity: id, Status: status, Code: code, ExitKnown: true, ExitCode: exit, CleanupConfirmed: true, Origin: "execution"}
	candidate := domain.HookFeedbackCandidate{}
	if body != "" {
		var err error
		candidate, err = domain.NewHookFeedbackCandidate(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	frozen, err := tool.FreezePostFeedback(p, prior, stats, candidate)
	if err != nil {
		t.Fatal(err)
	}
	return frozen
}
func appendPostStart(t *testing.T, f *replayFixture, p domain.PostHookPlan, ordinal int) {
	t.Helper()
	id, err := domain.NewPostHookRunIdentity(p, domain.HookPostToolUse, ordinal)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := NewHookExecutionStartedDraft(testTurnID, id)
	if err != nil {
		t.Fatal(err)
	}
	f.appendBatch(draft)
}
func postTerminalDraft(t *testing.T, p domain.PostHookPlan, result tool.InvocationResult, finished []domain.HookFinished, disposition domain.PostHookDisposition, code domain.HookCode, origin string) RecordDraft {
	t.Helper()
	feedback := make([]domain.FrozenHookFeedback, len(finished))
	for i, f := range finished {
		feedback[i] = f.Feedback
	}
	text, err := tool.RenderPostFeedback(feedback)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := domain.NewPostHookBudget(p.Allowance, p.Reservation, result.Budget().ActualBytes(), len(text))
	if err != nil {
		t.Fatal(err)
	}
	admission, _ := tool.NewExecutionAdmission(result.InvocationID(), 1)
	route, err := tool.SelectPostHookRoute(result, admission)
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := domain.NewPostHookTerminal(p, route, origin, disposition, code, len(finished), nil, budget, domain.HookDigest([]byte(text)))
	if err != nil {
		t.Fatal(err)
	}
	draft, err := NewToolPostHookCompletedDraft(testTurnID, terminal)
	if err != nil {
		t.Fatal(err)
	}
	return draft
}
func TestPostReplayIndependentErrorPrefixAndFrozenEvidence(t *testing.T) {
	f, inv, p := postReplayFixture(t, domain.HookFeedbackModel, domain.HookFeedbackModel)
	result := appendPostToolSuccess(t, f, inv, p)
	appendPostStart(t, f, p, 0)
	first := postFinished(t, p, nil, domain.HookError, domain.HookExitFailed, "")
	draft, _ := NewHookExecutionFinishedDraft(testTurnID, first)
	f.appendBatch(draft)
	appendPostStart(t, f, p, 1)
	second := postFinished(t, p, []domain.FrozenHookFeedback{first.Feedback}, domain.HookContinue, domain.HookOK, "accepted feedback")
	draft, _ = NewHookExecutionFinishedDraft(testTurnID, second)
	f.appendBatch(draft, postTerminalDraft(t, p, result, []domain.HookFinished{first, second}, domain.PostHookCompleted, domain.HookExitFailed, "execution"))
	plan, err := NewReplayPlanner().Plan(f.loaded())
	if err != nil {
		t.Fatal(err)
	}
	call := plan.ToolRecovery.Calls()[0]
	if call.Result.Status() != tool.ResultSuccess || call.PostHooks == nil || len(call.PostHooks.Finished) != 2 || call.PostHooks.Terminal == nil || len(plan.Tools[0].Hooks) != 2 || plan.Tools[0].DetailSequence == 0 {
		t.Fatal("lost post lifecycle/result")
	}
	selection, err := call.PostHooks.Selection(call)
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := tool.NewPostModelOutput(call.Result, selection, call.PostHooks.Feedback())
	if err != nil || !strings.Contains(outputs.Text(), "accepted feedback") {
		t.Fatal("lost output", err)
	}
	clone := plan.ToolRecovery.Calls()
	clone[0].PostHooks.Finished[0].Feedback.Content = "mutated"
	if plan.ToolRecovery.Calls()[0].PostHooks.Finished[0].Feedback.Content != "" {
		t.Fatal("mutable post recovery")
	}
}
func TestPostReplayRejectsInvalidAdmissionAndAtomicBatches(t *testing.T) {
	for _, bad := range []string{"start-before-result", "duplicate-plan", "wrong-budget", "wrong-feedback-digest", "cross-event", "missing-terminal", "split-final", "wrong-reference", "outputs-before-terminal", "denial-with-start"} {
		t.Run(bad, func(t *testing.T) {
			f, inv, p := postReplayFixture(t, domain.HookFeedbackModel)
			switch bad {
			case "start-before-result":
				appendPostStart(t, f, p, 0)
			case "duplicate-plan":
				d, _ := NewToolPostHookPlannedDraft(testTurnID, p)
				f.appendBatch(d)
			case "wrong-budget":
				result := appendPostToolSuccess(t, f, inv, p)
				terminal := postTerminalDraft(t, p, result, nil, domain.PostHookCancelled, domain.HookPostInterrupted, "recovery")
				value, _ := domain.DecodePostHookTerminal(terminal.PayloadBytes())
				budget, _ := domain.NewPostHookBudget(p.Allowance, p.Reservation, result.Budget().ActualBytes()+1, 0)
				value, _ = domain.NewPostHookTerminal(p, domain.PostHookRouteSuccess, "recovery", value.Disposition, value.Code, 0, nil, budget, domain.HookDigest(nil))
				d, _ := NewToolPostHookCompletedDraft(testTurnID, value)
				f.appendBatch(d)
			case "outputs-before-terminal":
				appendPostToolSuccess(t, f, inv, p)
				f.appendBatch(mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()))
			case "cross-event":
				appendPostToolSuccess(t, f, inv, p)
				wrongPlan, _ := domain.NewHookPlan(p.InvocationID, p.Capability, p.Branches[0].Hooks)
				id, _ := domain.NewHookRunIdentity(wrongPlan, 0)
				d, _ := NewHookExecutionStartedDraft(testTurnID, id)
				f.appendBatch(d)
			case "denial-with-start":
				denied := tool.NewInvocationErrorResult(inv, tool.ResultDenied, string(domain.HookTrustDenied), "denied")
				denied, _ = tool.FinalizeResult(denied, p.Allowance-p.Reservation, tool.BudgetPreserved)
				d, _ := NewToolCallResultDraft(testTurnID, denied)
				f.appendBatch(d)
				appendPostStart(t, f, p, 0)
			default:
				result := appendPostToolSuccess(t, f, inv, p)
				appendPostStart(t, f, p, 0)
				finished := postFinished(t, p, nil, domain.HookContinue, domain.HookOK, "feedback")
				if bad == "wrong-reference" {
					ref := finished.Feedback.Reference
					ref.ID = "forged"
					finished.Feedback.Reference = ref
				}
				d, _ := NewHookExecutionFinishedDraft(testTurnID, finished)
				if bad == "split-final" || bad == "missing-terminal" {
					f.appendBatch(d)
				} else {
					terminal := postTerminalDraft(t, p, result, []domain.HookFinished{finished}, domain.PostHookCompleted, domain.HookOK, "execution")
					if bad == "wrong-feedback-digest" {
						value, _ := domain.DecodePostHookTerminal(terminal.PayloadBytes())
						value, err := domain.NewPostHookTerminal(p, domain.PostHookRouteSuccess, "execution", value.Disposition, value.Code, value.Processed, value.SuffixFeedback, value.Budget, domain.HookDigest(nil))
						if err != nil {
							t.Fatal(err)
						}
						terminal, err = NewToolPostHookCompletedDraft(testTurnID, value)
						if err != nil {
							t.Fatal(err)
						}
					}
					f.appendBatch(d, terminal)
				}
			}
			if _, err := NewReplayPlanner().Plan(f.loaded()); err == nil {
				t.Fatal("corrupt post lifecycle accepted", bad)
			}
		})
	}
}
func TestPostRecoveryDistinguishesUnstartedSuffixFromAcceptedRun(t *testing.T) {
	for _, started := range []bool{false, true} {
		f, inv, p := postReplayFixture(t, domain.HookFeedbackModel, domain.HookFeedbackModel)
		result := appendPostToolSuccess(t, f, inv, p)
		appendPostStart(t, f, p, 0)
		first := postFinished(t, p, nil, domain.HookContinue, domain.HookOK, "durable prefix")
		d, _ := NewHookExecutionFinishedDraft(testTurnID, first)
		f.appendBatch(d)
		if started {
			appendPostStart(t, f, p, 1)
		}
		plan, err := NewReplayPlanner().Plan(f.loaded())
		if err != nil {
			t.Fatal(err)
		}
		g := plan.ToolRecovery.Calls()[0].PostHooks
		if (g.Started != nil) != started || len(g.Finished) != 1 || g.Terminal != nil || result.Status() != tool.ResultSuccess {
			t.Fatal("collapsed recovery states")
		}
	}
}

func TestPreZeroAndPostZeroHaveIndependentHistoryAssociations(t *testing.T) {
	f, inv, pre := hookReplayFixture(t, 1)
	started, id := hookStarted(t, pre, 0)
	f.appendBatch(started)
	f.appendBatch(hookFinished(t, pre, id, domain.HookContinue, domain.HookOK), hookDecision(t, pre, domain.HookContinue, domain.HookOK, 1, -1))
	d, err := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPostToolUse, domain.HookFeedbackModel, strings.Repeat("a", 64), "post", 7, "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := domain.NewHookPlan(string(inv.InvocationID()), domain.HookPatch, []domain.HookReference{d.Reference})
	currentPostBranch2, _ := domain.NewPostHookBranch(plan.Event, plan.Hooks, 16<<10)
	currentPostBranch2.Delivery = domain.HookDeliveryAvailable
	post, _ := domain.NewPostHookPlan(plan.InvocationID, plan.Capability, []domain.PostHookBranch{currentPostBranch2}, 256<<10, 16<<10)
	draft, _ := NewToolPostHookPlannedDraft(testTurnID, post)
	f.appendBatch(draft)
	result := appendPostToolSuccess(t, f, inv, post)
	appendPostStart(t, f, post, 0)
	finished := postFinished(t, post, nil, domain.HookContinue, domain.HookOK, "post zero feedback")
	draft, _ = NewHookExecutionFinishedDraft(testTurnID, finished)
	f.appendBatch(draft, postTerminalDraft(t, post, result, []domain.HookFinished{finished}, domain.PostHookCompleted, domain.HookOK, "execution"))
	replay, err := NewReplayPlanner().Plan(f.loaded())
	if err != nil {
		t.Fatal(err)
	}
	hooks := replay.Tools[0].Hooks
	if len(hooks) != 2 || hooks[0].Result.Identity.Ordinal != 0 || hooks[1].Result.Identity.Ordinal != 0 || hooks[0].Result.Identity.Event != domain.HookPreToolUse || hooks[1].Result.Identity.Event != domain.HookPostToolUse || hooks[0].Result.Identity.RunID == hooks[1].Result.Identity.RunID || replay.Tools[0].PostHooks.Terminal == nil {
		t.Fatal("pre/post ordinal zero collided")
	}
}
