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

func singleRewriteRuntime(t *testing.T, j *fakeJournal, withPost bool) (*Runtime, tool.Invocation, tool.Invocation, *postTestRunner, func() int, func() int) {
	t.Helper()
	r, original, _, _ := hookRuntimeFixture(t, j, 0)
	input := rewrittenFixtureInput(t, original, false)
	effective, err := tool.RewriteInvocation(original, input)
	if err != nil {
		t.Fatal(err)
	}
	prepare, execute, _ := rewriteCandidateFixture(t, r, effective)
	d, _ := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPreToolUse, domain.HookFeedbackNone, domain.HookDigest(nil), "rewrite", 7, "true", 5)
	ds := []domain.HookDeclaration{d}
	if withPost {
		for _, event := range []domain.HookEvent{domain.HookPostToolUse, domain.HookPostToolUseFailure} {
			post, _ := domain.NewHookDeclaration(domain.HookSourceProject, event, domain.HookFeedbackModel, domain.HookDigest(nil), map[domain.HookEvent]string{domain.HookPostToolUse: "post", domain.HookPostToolUseFailure: "failure"}[event], 7, "true", 5)
			ds = append(ds, post)
		}
	}
	snapshot, err := domain.NewHookSnapshot(ds, nil)
	if err != nil {
		t.Fatal(err)
	}
	runner := &postTestRunner{run: func(_ context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, in domain.HookInput, _ time.Duration) domain.HookExecution {
		stats := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
		if id.Event.Post() {
			return domain.HookExecution{Result: stats}
		}
		replacement, err := domain.NewHookInputReplacement(input, in.RewriteContext().Profile())
		if err != nil {
			t.Fatal(err)
		}
		return domain.HookExecution{Result: stats, Replacement: replacement}
	}}
	if err := r.InstallHooks(snapshot, runner, "/workspace", domain.HookOK); err != nil {
		t.Fatal(err)
	}
	r.resultBudget = tool.NewResultBudget()
	if err := r.resultBudget.BeginSample(1); err != nil {
		t.Fatal(err)
	}
	return r, original, effective, runner, prepare, execute
}

func TestPreRewriteLateCancellationPreservesDefiniteEffectAndFinalGate(t *testing.T) {
	for _, point := range []string{"runner-return", "finished-sync", "ordinary-allow", "started-accepted"} {
		t.Run(point, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			j := &fakeJournal{}
			r, original, effective, runner, prepare, execute := singleRewriteRuntime(t, j, false)
			if point == "runner-return" {
				run := runner.run
				runner.run = func(ctx context.Context, id domain.HookRunIdentity, d domain.HookDeclaration, in domain.HookInput, limit time.Duration) domain.HookExecution {
					out := run(ctx, id, d, in, limit)
					cancel()
					return out
				}
			}
			j.hook = func(batch []session.RecordDraft) {
				for _, draft := range batch {
					match := point == "finished-sync" && draft.EventKind() == session.EventHookExecutionFinished || point == "ordinary-allow" && draft.EventKind() == session.EventToolApprovalDecided || point == "started-accepted" && draft.EventKind() == session.EventToolExecutionStarted
					if match {
						cancel()
					}
				}
			}
			result, _, err := r.executeApproved(ctx, postApprovalEmitter(t, r, original, protocol.HookTrustSession, tool.ApprovalAllowOnce), runtimeTurnID, original)
			if err != nil || runner.calls != 1 || r.poisoned.Load() {
				t.Fatal("definite cancellation failed", err)
			}
			state := r.toolCalls[original.InvocationID()]
			if state.pre == nil || state.pre.Decision == nil || state.pre.Decision.Status != domain.HookContinue || len(state.pre.Finished) != 1 || state.pre.Finished[0].InputEffect.Kind() != domain.HookInputReplaced || countRewriteFact(t, j, session.EventToolHookGateDecided) != 1 {
				t.Fatal("late cancellation rewrote completed gate")
			}
			if point == "started-accepted" {
				if execute() != 1 || state.admission.Sequence() == 0 || !strings.Contains(result.Preview().Text(), "Admitted tool attempt") {
					t.Fatal("accepted attempt lost full disclosure")
				}
			} else {
				if execute() != 0 || state.admission.Sequence() != 0 || result.Status() != tool.ResultCancelled || strings.Contains(result.Preview().Text(), "Admitted tool attempt") || !strings.Contains(result.Preview().Text(), "not executed") {
					t.Fatal("cancelled input claimed execution", result.Preview().Text())
				}
				if point != "ordinary-allow" && prepare() != 0 {
					t.Fatal("candidate prepared after completed gate cancellation")
				}
			}
			if tool.ValidateResultInput(effective, result) != nil {
				t.Fatal("cancellation used original result input")
			}
			batches := j.snapshot()
			for _, batch := range batches {
				if batch[0].EventKind() == session.EventHookExecutionFinished && (len(batch) != 2 || batch[1].EventKind() != session.EventToolHookGateDecided) {
					t.Fatal("last finished and continue separated")
				}
			}
		})
	}
}

func leaveRewriteAllowance(t *testing.T, r *Runtime, remaining int) {
	t.Helper()
	r.resultBudget = tool.NewResultBudget()
	if err := r.resultBudget.BeginSample(3); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		allowance, err := r.resultBudget.Allowance(tool.CapabilityRead)
		if err != nil {
			t.Fatal(err)
		}
		actual := allowance
		if i == 1 {
			actual = tool.SamplePreviewBytes - r.resultBudget.SampleUsed() - remaining
		}
		metadata, err := tool.NewResultBudgetMetadata(allowance, actual, tool.BudgetPreserved)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.resultBudget.Commit(tool.CapabilityRead, metadata); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPreRewriteLowBudgetStopsBeforeCandidateAndUsesSingleTicket(t *testing.T) {
	for _, withPost := range []bool{false, true} {
		for _, denyPost := range []bool{false, true} {
			if denyPost && !withPost {
				continue
			}
			t.Run(map[bool]string{true: "post", false: "plain"}[withPost]+map[bool]string{true: "-deny", false: "-allow"}[denyPost], func(t *testing.T) {
				j := &fakeJournal{}
				r, original, effective, runner, prepare, execute := singleRewriteRuntime(t, j, withPost)
				leaveRewriteAllowance(t, r, tool.SafetyResultBytes)
				before := r.resultBudget.SampleUsed()
				approvals := 0
				trust := hookTrustEmitter(t, r, protocol.HookTrustSession, nil)
				emit := func(e protocol.Event) {
					if e.Kind == protocol.EventToolApprovalRequested {
						approvals++
					}
					if e.Kind == protocol.EventHookTrustRequested {
						p, err := protocol.DecodeHookTrustRequested(e)
						if err != nil {
							t.Fatal(err)
						}
						if denyPost && p.Declaration.Reference.Event.Post() {
							hookTrustEmitter(t, r, protocol.HookTrustDeny, nil)(e)
							return
						}
					}
					trust(e)
				}
				result, _, err := r.executeApproved(context.Background(), emit, runtimeTurnID, original)
				if err != nil || r.poisoned.Load() || runner.calls != 1 || prepare() != 0 || execute() != 0 || approvals != 0 {
					t.Fatal("budget admitted side effect", err)
				}
				want := tool.HookRewriteBudgetExceeded
				if denyPost {
					want = string(domain.HookTrustDenied)
				}
				if result.Code() != want || result.Budget().ActualBytes() > tool.SafetyResultBytes || !strings.Contains(result.Preview().Text(), "not executed") || strings.Contains(result.Preview().Text(), "old\\n") || tool.ValidateResultInput(effective, result) != nil {
					t.Fatal("unsafe budget terminal", result.Code(), result.Preview().Text())
				}
				if r.resultTicket != nil || r.resultBudget.SampleUsed() != before+result.Budget().ActualBytes() || countRewriteFact(t, j, session.EventToolCallResult) != 1 {
					t.Fatal("ticket or note counted twice")
				}
				if withPost {
					post := r.hooks.post[original.InvocationID()]
					if post == nil || post.terminal == nil || post.terminal.Route != domain.PostHookRouteNone || len(post.finished) != 0 {
						t.Fatal("unadmitted failure fabricated post execution")
					}
				} else if countRewriteFact(t, j, session.EventToolPostHookPlanned) != 0 {
					t.Fatal("post plan fabricated to reserve input note")
				}
				used, batches := r.resultBudget.SampleUsed(), len(j.snapshot())
				again, _, err := r.executeApproved(context.Background(), emit, runtimeTurnID, original)
				if err != nil || again.Preview() != result.Preview() || len(j.snapshot()) != batches || r.resultBudget.SampleUsed() != used {
					t.Fatal("frozen safety result refrozen", err)
				}
			})
		}
	}
}

type failingPreHistory struct{ calls, failAt int }

func (h *failingPreHistory) PreHooksCommitted(tool.Invocation, uint64, session.ReplayedHookGate) error {
	h.calls++
	if h.calls == h.failAt {
		return errors.New("fixture host install failed")
	}
	return nil
}
func (*failingPreHistory) PostHooksCommitted(tool.InvocationID, uint64, session.ReplayedPostHooks) error {
	return nil
}

func TestPreRewriteDurableInstallAndSyncFailuresPoisonBeforeOrdinaryEffects(t *testing.T) {
	for _, hostFailure := range []bool{false, true} {
		for _, at := range []int{1, 2, 3} {
			t.Run(map[bool]string{true: "host", false: "sync"}[hostFailure]+string(rune('0'+at)), func(t *testing.T) {
				j := &fakeJournal{}
				r, original, _, runner, prepare, execute := singleRewriteRuntime(t, j, false)
				emissions := 0
				r.activityEmitter = func(e protocol.Event) {
					p, err := protocol.DecodeHookActivity(e)
					state := r.toolCalls[original.InvocationID()]
					if err != nil || state.pre == nil || state.detailSequence != p.PreInput.Sequence() || !state.original.Equal(original) {
						t.Fatal("activity preceded committed snapshot installation", err)
					}
					emissions++
				}
				if hostFailure {
					r.hookHistory = &failingPreHistory{failAt: at}
				} else {
					j.failAt = at
				}
				_, _, err := r.executeApproved(context.Background(), postApprovalEmitter(t, r, original, protocol.HookTrustSession, tool.ApprovalAllowOnce), runtimeTurnID, original)
				if err == nil || !r.poisoned.Load() || emissions != at-1 || prepare() != 0 || execute() != 0 || countRewriteFact(t, j, session.EventToolApprovalDecided) != 0 || countRewriteFact(t, j, session.EventToolExecutionStarted) != 0 {
					t.Fatal("failed install continued", err)
				}
				before, runs := len(j.snapshot()), runner.calls
				if _, _, err := r.executeApproved(context.Background(), nil, runtimeTurnID, original); err == nil || runner.calls != runs || len(j.snapshot()) != before {
					t.Fatal("poisoned rewrite reran")
				}
			})
		}
	}
}

func TestPreRewriteOrdinaryDenyAbortAndCandidateFailureUseShortUnadmittedNote(t *testing.T) {
	for _, withPost := range []bool{false, true} {
		for _, mode := range []string{"deny", "abort", "prepare-failed", "post-trust-denied"} {
			if mode == "post-trust-denied" && !withPost {
				continue
			}
			t.Run(mode+map[bool]string{true: "-post", false: "-plain"}[withPost], func(t *testing.T) {
				j := &fakeJournal{}
				r, original, effective, runner, _, execute := singleRewriteRuntime(t, j, withPost)
				if mode == "prepare-failed" {
					r.patchPreparer = postFailedPatchPreparation{}
				}
				approval := tool.ApprovalDeny
				if mode == "abort" {
					approval = tool.ApprovalAbort
				}
				base := postApprovalEmitter(t, r, original, protocol.HookTrustSession, approval)
				emit := func(e protocol.Event) {
					if e.Kind == protocol.EventHookTrustRequested && mode == "post-trust-denied" {
						p, _ := protocol.DecodeHookTrustRequested(e)
						if p.Declaration.Reference.Event.Post() {
							hookTrustEmitter(t, r, protocol.HookTrustDeny, nil)(e)
							return
						}
					}
					base(e)
				}
				result, aborted, err := r.executeApproved(context.Background(), emit, runtimeTurnID, original)
				if err != nil || aborted != (mode == "abort") || execute() != 0 || runner.calls != 1 || r.poisoned.Load() || countRewriteFact(t, j, session.EventToolExecutionStarted) != 0 || tool.ValidateResultInput(effective, result) != nil {
					t.Fatal("unadmitted failure executed or lost E metadata", err)
				}
				if !strings.Contains(result.Preview().Text(), "tool was not executed") || strings.Contains(result.Preview().Text(), "Admitted tool attempt") || result.Budget().ActualBytes() > tool.SafetyResultBytes {
					t.Fatal("unadmitted failure disclosed full input")
				}
				if withPost {
					post := r.hooks.post[original.InvocationID()]
					if post == nil || post.terminal == nil || post.terminal.Route != domain.PostHookRouteNone || len(post.finished) != 0 {
						t.Fatal("ordinary denial fabricated post route")
					}
				}
			})
		}
	}
}

func TestPreRewriteCancellationAfterIntermediateSyncKeepsCandidateUnselected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	j := &fakeJournal{}
	r, original, _, runner, prepare, execute := singleRewriteRuntime(t, j, false)
	r.hooks.mu.Lock()
	ds := r.hooks.snapshot.Declarations()
	r.hooks.mu.Unlock()
	next, err := domain.NewHookDeclaration(ds[0].Reference.Source, ds[0].Reference.Event, ds[0].Reference.Feedback, ds[0].Reference.Revision, "last", 7, ds[0].Command, ds[0].Reference.TimeoutSeconds)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewHookSnapshot(append(ds, next), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.InstallHooks(snapshot, runner, "/workspace", domain.HookOK); err != nil {
		t.Fatal(err)
	}
	j.hook = func(batch []session.RecordDraft) {
		if batch[0].EventKind() == session.EventHookExecutionFinished {
			cancel()
		}
	}
	result, _, err := r.executeApproved(ctx, postApprovalEmitter(t, r, original, protocol.HookTrustSession, tool.ApprovalAllowOnce), runtimeTurnID, original)
	if err == nil || result.Status() != tool.ResultCancelled || prepare() != 0 || execute() != 0 || runner.calls != 1 || countRewriteFact(t, j, session.EventHookExecutionStarted) != 1 || strings.HasPrefix(result.Preview().Text(), "[PreToolUse") {
		t.Fatal("intermediate cancellation selected or executed C", err)
	}
	state := r.toolCalls[original.InvocationID()]
	if len(state.pre.Finished) != 1 || state.pre.Decision == nil || state.pre.Decision.Status != domain.HookCancel || state.pre.Finished[0].InputEffect.Kind() != domain.HookInputReplaced || tool.ValidateResultInput(original, result) != nil {
		t.Fatal("real intermediate effect or original failure metadata lost")
	}
}
