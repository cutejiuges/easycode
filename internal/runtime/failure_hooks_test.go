package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

func TestFailureBranchesRespectOriginalOutcomesForAllCapabilities(t *testing.T) {
	for _, cap := range []tool.CapabilityID{tool.CapabilityPatch, tool.CapabilityWrite, tool.CapabilityExec} {
		for _, mode := range []string{"prepare", "denied", "cancelled", "error", "conflict", "uncertain", "success"} {
			if cap == tool.CapabilityExec && mode == "conflict" {
				continue
			}
			t.Run(string(cap)+"/"+mode, func(t *testing.T) {
				j := &fakeJournal{}
				r, inv, runner, _, _ := postRuntimeFixture(t, j, domain.HookFeedbackModel)
				installFailureBranches(t, r, runner, true)
				status := tool.ResultError
				switch mode {
				case "denied":
					status = tool.ResultDenied
				case "cancelled":
					status = tool.ResultCancelled
				case "conflict":
					status = tool.ResultConflict
				case "uncertain":
					status = tool.ResultOutcomeUncertain
				case "success":
					status = tool.ResultSuccess
				}
				switch cap {
				case tool.CapabilityPatch:
					if mode != "success" {
						r.patchExecutor = postPatchOutcome{status}
					}
					if mode == "prepare" {
						r.patchPreparer = postFailedPatchPreparation{}
					}
				case tool.CapabilityWrite:
					var candidate tool.WriteCandidate
					inv, candidate = runtimeWriteInvocation(t)
					r.writePreparer = &fixedWritePreparer{candidate: candidate}
					r.writeExecutor = &fixedWriteExecutor{}
					if mode != "success" {
						r.writeExecutor = &fixedWriteExecutor{resultOverride: func(c tool.WriteCandidate) tool.InvocationResult {
							return tool.NewWriteCandidateResult(c, status, "fixture_outcome")
						}}
					}
					if mode == "prepare" {
						r.writePreparer = postFailedWritePreparation{}
					}
				case tool.CapabilityExec:
					_, execInv, components := runtimeExecFixture(t, j)
					inv = execInv
					r.execPreparer, r.execExecutor = components, components
					if mode != "success" {
						components.resultOverride = func(c tool.ExecCandidate) tool.InvocationResult {
							return tool.NewExecErrorResult(c.Invocation(), status, "fixture_outcome", "PRIVATE-ORIGINAL-ERROR")
						}
					}
					if mode == "prepare" {
						r.execPreparer = postFailedExecPreparation{}
					}
				}
				expected := domain.PostHookRouteNone
				if mode == "error" || mode == "conflict" {
					expected = domain.PostHookRouteFailure
				}
				if mode == "success" {
					expected = domain.PostHookRouteSuccess
				}
				runner.run = func(_ context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, input domain.HookInput, _ time.Duration) domain.HookExecution {
					event, _ := expected.Event()
					if id.Event != event || input.Event != event {
						t.Fatal("wrong post branch", id.Event, expected)
					}
					if expected == domain.PostHookRouteFailure {
						b, err := input.Bytes()
						if err != nil || input.Response != nil || input.IsInterrupt == nil || *input.IsInterrupt || strings.Contains(string(b), "PRIVATE-ORIGINAL-ERROR") || !strings.HasPrefix(input.Error, "Tool execution failed: ") {
							t.Fatal("unsafe failure input", err)
						}
					}
					feedback, _ := domain.NewHookFeedbackCandidate("authorized diagnosis")
					return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}, Feedback: feedback}
				}
				approval := tool.ApprovalAllowOnce
				if mode == "denied" {
					approval = tool.ApprovalDeny
				}
				trustCount := 0
				emit := postApprovalEmitter(t, r, inv, protocol.HookTrustSession, approval)
				wrapped := func(e protocol.Event) {
					if e.Kind == protocol.EventHookTrustRequested {
						trustCount++
					}
					if e.Kind == protocol.EventToolApprovalRequested && trustCount != 2 {
						t.Fatal("tool approval preceded branch trust")
					}
					emit(e)
				}
				result, _, err := r.executeApproved(context.Background(), wrapped, runtimeTurnID, inv)
				state := r.hooks.post[inv.InvocationID()]
				if result.Validate() != nil || result.Status() != status || trustCount != 2 {
					t.Fatal("original outcome/trust changed", result.Status(), err, trustCount)
				}
				if mode == "uncertain" {
					if err == nil || runner.calls != 0 || state.terminal != nil || !r.poisoned.Load() {
						t.Fatal("uncertain result crossed barrier")
					}
					return
				}
				if err != nil || state.terminal == nil || state.route != expected || r.resultTicket != nil {
					t.Fatal("wrong terminal", err, state.route)
				}
				want := 0
				if expected != domain.PostHookRouteNone {
					want = 1
				}
				if runner.calls != want || len(state.finished) != want {
					t.Fatal("branches were not exclusive", runner.calls)
				}
				before := len(j.snapshot())
				if _, _, err := r.executeApproved(context.Background(), wrapped, runtimeTurnID, inv); err != nil || len(j.snapshot()) != before || runner.calls != want {
					t.Fatal("replay repeated a side effect", err)
				}
			})
		}
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
			r.patchExecutor = postPatchOutcome{tool.ResultError}
			result, _, err := r.executeApproved(ctx, postApprovalEmitter(t, r, inv, protocol.HookTrustSession, tool.ApprovalAllowOnce), runtimeTurnID, inv)
			state := r.hooks.post[inv.InvocationID()]
			if result.Status() != tool.ResultError || state.route != domain.PostHookRouteFailure || runner.calls != 0 || state.terminal == nil || r.resultTicket != nil {
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

func TestUnselectedModelBranchStillReservesOriginalBase(t *testing.T) {
	j := &fakeJournal{}
	r, inv, runner, _, _ := postRuntimeFixture(t, j, domain.HookFeedbackNone)
	installFailureBranches(t, r, runner, true)
	result, _, err := r.executeApproved(context.Background(), postApprovalEmitter(t, r, inv, protocol.HookTrustSession, tool.ApprovalAllowOnce), runtimeTurnID, inv)
	state := r.hooks.post[inv.InvocationID()]
	if err != nil || result.Status() != tool.ResultSuccess || result.Budget().Allowance() != 240<<10 || state.plan.Reservation != 16<<10 || state.terminal.Route != domain.PostHookRouteSuccess || runner.calls != 1 || r.resultTicket != nil {
		t.Fatal("unselected model reservation changed base", err)
	}
	text, err := tool.RenderPostFeedback(postFeedback(state))
	if err != nil || text != "" || state.terminal.Budget.Released != 16<<10 {
		t.Fatal("unselected branch disclosed a notice", err)
	}
}

func TestFailureEverySyncFaultPreventsCachedReadmission(t *testing.T) {
	for _, kind := range []session.EventKind{session.EventToolPostHookPlanned, session.EventToolCallResult, session.EventHookExecutionStarted, session.EventHookExecutionFinished, session.EventToolPostHookCompleted} {
		for _, after := range []bool{false, true} {
			t.Run(string(kind)+map[bool]string{false: "/before", true: "/after"}[after], func(t *testing.T) {
				j := &hookFaultJournal{kind: kind, after: after, failure: errors.New("injected Sync failure")}
				r, inv, runner, _, _ := postRuntimeFixture(t, j, domain.HookFeedbackModel)
				installFailureBranches(t, r, runner, true)
				executor := &fixedPatchExecutor{outcome: tool.ResultError}
				r.patchExecutor = executor
				emit := postApprovalEmitter(t, r, inv, protocol.HookTrustSession, tool.ApprovalAllowOnce)
				if _, _, err := r.executeApproved(context.Background(), emit, runtimeTurnID, inv); err == nil || !r.poisoned.Load() {
					t.Fatal("failure Sync did not poison", err)
				}
				runs, effects, batches := runner.calls, executor.calls, len(j.snapshot())
				if _, _, err := r.executeApproved(context.Background(), emit, runtimeTurnID, inv); err == nil || runs != runner.calls || effects != executor.calls || batches != len(j.snapshot()) {
					t.Fatal("poisoned cached invocation crossed admission")
				}
				if kind == session.EventToolPostHookPlanned || kind == session.EventToolCallResult || kind == session.EventHookExecutionStarted {
					if runner.calls != 0 {
						t.Fatal("hook crossed unconfirmed prerequisite")
					}
				}
				results := 0
				for _, batch := range j.snapshot() {
					for _, d := range batch {
						if d.EventKind() == session.EventToolCallResult {
							results++
							p, err := session.DecodeToolCallResultPayload(session.Record{EventKind: d.EventKind(), Payload: d.PayloadBytes()})
							if err != nil || p.Status != tool.ResultError {
								t.Fatal("original failure rewritten", err)
							}
						}
					}
				}
				if results > 1 || executor.calls > 1 {
					t.Fatal("duplicate original effect/result")
				}
			})
		}
	}
}
func TestBothAlternateBranchesTrustAllSixteenBeforeCandidate(t *testing.T) {
	j := &fakeJournal{}
	r, inv, runner, preparer, executor := postRuntimeFixture(t, j, domain.HookFeedbackModel)
	var ds []domain.HookDeclaration
	for e, event := range []domain.HookEvent{domain.HookPostToolUse, domain.HookPostToolUseFailure} {
		for i := 0; i < domain.MaxHookMatches; i++ {
			d, err := domain.NewHookDeclaration(domain.HookSourceProject, event, domain.HookFeedbackModel, strings.Repeat("a", 64), string(rune('a'+e))+string(rune('a'+i)), 7, "true", 5)
			if err != nil {
				t.Fatal(err)
			}
			ds = append(ds, d)
		}
	}
	snapshot, err := domain.NewHookSnapshot(ds, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.InstallHooks(snapshot, runner, "/workspace", domain.HookOK); err != nil {
		t.Fatal(err)
	}
	trusts := 0
	emit := postApprovalEmitter(t, r, inv, protocol.HookTrustSession, tool.ApprovalAllowOnce)
	wrapped := func(e protocol.Event) {
		if e.Kind == protocol.EventHookTrustRequested {
			trusts++
			if preparer.calls != 0 || executor.calls != 0 {
				t.Fatal("trust after preparation")
			}
		}
		if e.Kind == protocol.EventToolApprovalRequested && trusts != 16 {
			t.Fatal("alternate branch skipped trust", trusts)
		}
		emit(e)
	}
	result, _, err := r.executeApproved(context.Background(), wrapped, runtimeTurnID, inv)
	if err != nil || result.Status() != tool.ResultSuccess || trusts != 16 || runner.calls != 8 || executor.calls != 1 {
		t.Fatal("sixteen branch trust was not exact", err, trusts, runner.calls)
	}
}
