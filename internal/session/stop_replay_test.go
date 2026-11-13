package session

import (
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
)

func testStopReplayPlan(t *testing.T, index uint32, count uint8, policies ...domain.HookFeedbackPolicy) domain.StopPlan {
	t.Helper()
	refs := make([]domain.HookReference, len(policies))
	for i, policy := range policies {
		d, err := domain.NewStopHookDeclaration(domain.HookSourceUser, policy, domain.HookDigest(nil), "completion-"+string(rune('a'+i)), "true", 5)
		if err != nil {
			t.Fatal(err)
		}
		refs[i] = d.Reference
	}
	p, err := domain.NewStopChecksPlan(domain.AssistantSampleReference{TurnID: testTurnID, SampleIndex: index}, count, refs)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func testStopReplayFixture(t *testing.T, p domain.StopPlan) *replayFixture {
	t.Helper()
	f := newReplayFixture(t)
	f.appendMetadata()
	f.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
	f.appendInitialInputs(testTurnID)
	testAppendStopSample(t, f, p)
	return f
}
func testAppendStopSample(t *testing.T, f *replayFixture, p domain.StopPlan) {
	t.Helper()
	draft, err := NewTurnStopHookPlannedDraft(p)
	if err != nil {
		t.Fatal(err)
	}
	f.appendBatch(mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()), mustDraft(t, EventSampleUsage, testTurnID, testSampleUsage(t)), draft)
}
func testStopFinished(t *testing.T, p domain.StopPlan, ordinal int, status domain.HookOutcome, code domain.HookCode, origin string) domain.HookFinished {
	t.Helper()
	id, err := domain.NewStopHookRunIdentity(p, ordinal)
	if err != nil {
		t.Fatal(err)
	}
	r := domain.HookRunResult{Identity: id, Status: status, Code: code, ExitKnown: true, CleanupConfirmed: true, Origin: origin}
	if status == domain.HookUncertain {
		r.ExitKnown = false
		r.CleanupConfirmed = false
	}
	if code == domain.HookExitFailed {
		r.ExitCode = 1
	}
	if status == domain.HookCancel {
		r.Cancelled = true
	}
	f, err := domain.NewEmptyHookFinished(r, p.Checks()[ordinal])
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func testAppendStopStart(t *testing.T, f *replayFixture, p domain.StopPlan, ordinal int) {
	t.Helper()
	id, err := domain.NewStopHookRunIdentity(p, ordinal)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := NewHookExecutionStartedDraft(testTurnID, id)
	if err != nil {
		t.Fatal(err)
	}
	f.appendBatch(draft)
}
func testStopDecisionDraft(t *testing.T, p domain.StopPlan, disposition domain.StopDisposition, code domain.HookCode, origin domain.HookOrigin, source domain.StopDecisionSource, finished []domain.HookFinished) RecordDraft {
	t.Helper()
	var feedback domain.StopFeedback
	if disposition == domain.StopContinued {
		c, _ := domain.NewHookFeedbackCandidate("repair")
		var err error
		feedback, err = domain.NewStopFeedback(finished[len(finished)-1].Result, p.Checks()[len(finished)-1], c)
		if err != nil {
			t.Fatal(err)
		}
	}
	decision, err := domain.NewStopDecision(p, disposition, code, origin, source, finished, feedback)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := NewTurnStopHookDecidedDraft(decision)
	if err != nil {
		t.Fatal(err)
	}
	return draft
}
func testAppendStopBlock(t *testing.T, f *replayFixture, p domain.StopPlan) {
	t.Helper()
	testAppendStopStart(t, f, p, 0)
	finished := testStopFinished(t, p, 0, domain.HookBlock, domain.HookDenied, "execution")
	draft, err := NewHookExecutionFinishedDraft(testTurnID, finished)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := domain.NewStopHookSource(0)
	f.appendBatch(draft, testStopDecisionDraft(t, p, domain.StopContinued, domain.HookDenied, domain.HookOriginExecution, source, []domain.HookFinished{finished}))
}

func TestStopReplaySeparatesSampleDecisionAndNativeDelivery(t *testing.T) {
	for _, steer := range []bool{false, true} {
		t.Run(map[bool]string{false: "feedback-only", true: "with-steering"}[steer], func(t *testing.T) {
			p := testStopReplayPlan(t, 0, 0, domain.HookFeedbackModel)
			f := testStopReplayFixture(t, p)
			plan, err := NewReplayPlanner().Plan(f.loaded())
			if err != nil || plan.StopRecovery == nil || plan.InterruptedTail != nil || plan.ToolRecovery != nil || len(plan.Turns) != 0 || len(plan.NativeCommits) != 2 || len(plan.SampleUsages) != 1 {
				t.Fatal("candidate mistaken for completion", err)
			}
			testAppendStopBlock(t, f, p)
			plan, err = NewReplayPlanner().Plan(f.loaded())
			if err != nil || plan.StopRecovery == nil || !plan.StopRecovery.State().PendingFeedback() || len(plan.NativeCommits) != 2 {
				t.Fatal("intent mistaken for delivery", err)
			}
			feedback := mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit())
			if steer {
				a, _ := domain.NewInputAssociation(testTurnID, domain.InputSteering, 1, []domain.InputID{domain.InputID(secondTurnID)})
				input, err := NewTurnInputsCommittedDraft(a)
				if err != nil {
					t.Fatal(err)
				}
				f.appendBatch(feedback, input, mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()))
			} else {
				f.appendBatch(feedback)
			}
			plan, err = NewReplayPlanner().Plan(f.loaded())
			if err != nil || plan.StopRecovery == nil || plan.StopRecovery.State().PendingFeedback() {
				t.Fatal("missing delivered state", err)
			}
			want := 3
			if steer {
				want++
			}
			if len(plan.NativeCommits) != want || plan.NativeCommits[2].Role != domain.NativeCommitHookFeedback || plan.NativeCommits[2].Feedback == nil || plan.NativeCommits[2].Inputs.TurnID() != "" {
				t.Fatal("wrong native provenance")
			}
			if steer && (plan.NativeCommits[3].Role != domain.NativeCommitUserInputs || plan.NativeCommits[3].Inputs.IDs()[0] != domain.InputID(secondTurnID)) {
				t.Fatal("real input lost")
			}
			next := testStopReplayPlan(t, 1, 1, domain.HookFeedbackModel)
			testAppendStopSample(t, f, next)
			testAppendStopStart(t, f, next, 0)
			finished := testStopFinished(t, next, 0, domain.HookContinue, domain.HookOK, "execution")
			done, err := NewHookExecutionFinishedDraft(testTurnID, finished)
			if err != nil {
				t.Fatal(err)
			}
			decision := testStopDecisionDraft(t, next, domain.StopPassed, domain.HookOK, domain.HookOriginExecution, domain.NewStopPlanSource(), []domain.HookFinished{finished})
			f.appendBatch(done, decision, mustDraft(t, EventTurnCompleted, testTurnID, TurnCompletedPayload{}))
			plan, err = NewReplayPlanner().Plan(f.loaded())
			if err != nil || plan.StopRecovery != nil || plan.ToolRecovery != nil || plan.InterruptedTail != nil || len(plan.Turns) != 1 || plan.Turns[0].State != ReplayedTurnCompleted || len(plan.SampleUsages) != 2 || len(plan.Stops) != 2 {
				t.Fatal("wrong final completion", err)
			}
		})
	}
}

func TestStopReplayRejectsSplitOrPrematureFacts(t *testing.T) {
	for _, mode := range []string{"sample", "input", "failure", "split-pass", "split-block", "decision-and-native", "duplicate-delivery", "standalone-steering"} {
		t.Run(mode, func(t *testing.T) {
			p := testStopReplayPlan(t, 0, 0, domain.HookFeedbackModel)
			f := testStopReplayFixture(t, p)
			switch mode {
			case "sample":
				testAppendStopSample(t, f, testStopReplayPlan(t, 1, 0, domain.HookFeedbackModel))
			case "input":
				f.appendInitialInputs(testTurnID)
			case "failure":
				f.appendBatch(mustDraft(t, EventTurnFailed, testTurnID, TurnFailedPayload{Code: "cancelled"}))
			case "split-pass", "split-block", "decision-and-native":
				testAppendStopStart(t, f, p, 0)
				status, code := domain.HookContinue, domain.HookOK
				if mode != "split-pass" {
					status, code = domain.HookBlock, domain.HookDenied
				}
				finished := testStopFinished(t, p, 0, status, code, "execution")
				draft, err := NewHookExecutionFinishedDraft(testTurnID, finished)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "decision-and-native" {
					source, _ := domain.NewStopHookSource(0)
					decision := testStopDecisionDraft(t, p, domain.StopContinued, domain.HookDenied, domain.HookOriginExecution, source, []domain.HookFinished{finished})
					f.appendBatch(draft, decision, mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()))
				} else {
					f.appendBatch(draft)
				}
			case "duplicate-delivery":
				testAppendStopBlock(t, f, p)
				f.appendBatch(mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()))
				f.appendBatch(mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()))
			case "standalone-steering":
				testAppendStopBlock(t, f, p)
				a, _ := domain.NewInputAssociation(testTurnID, domain.InputSteering, 1, []domain.InputID{domain.InputID(secondTurnID)})
				draft, _ := NewTurnInputsCommittedDraft(a)
				f.appendBatch(draft, mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()))
			}
			if _, err := NewReplayPlanner().Plan(f.loaded()); err == nil {
				t.Fatal("illegal stop transition accepted", mode)
			}
		})
	}
}

func TestStopReplayTerminalsPreserveActualRunAndRecoveryOrigin(t *testing.T) {
	for _, mode := range []string{"passed", "blocked", "error", "cancelled-after-block", "uncertain", "headless", "cancelled-before-run", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			p := testStopReplayPlan(t, 0, 0, domain.HookFeedbackNone)
			if mode == "unavailable" {
				var err error
				p, err = domain.NewUnavailableStopPlan(p.Sample(), 0, domain.HookConfigInvalid)
				if err != nil {
					t.Fatal(err)
				}
			}
			f := testStopReplayFixture(t, p)
			source := domain.NewStopPlanSource()
			origin := domain.HookOriginExecution
			disposition, code := domain.StopBlocked, domain.HookTrustRequired
			var results []domain.HookFinished
			var prefix []RecordDraft
			switch mode {
			case "headless":
			case "cancelled-before-run":
				disposition, code = domain.StopCancelled, domain.HookCancelled
			case "unavailable":
				disposition, code, origin = domain.StopError, domain.HookConfigInvalid, domain.HookOriginRecovery
			default:
				testAppendStopStart(t, f, p, 0)
				status, runCode, runOrigin := domain.HookBlock, domain.HookDenied, "execution"
				source, _ = domain.NewStopHookSource(0)
				switch mode {
				case "passed":
					status, runCode = domain.HookContinue, domain.HookOK
					source = domain.NewStopPlanSource()
					disposition, code = domain.StopPassed, domain.HookOK
				case "blocked":
					disposition, code = domain.StopBlocked, domain.HookDenied
				case "error":
					status, runCode = domain.HookError, domain.HookExitFailed
					disposition, code = domain.StopError, domain.HookExitFailed
				case "cancelled-after-block":
					disposition, code = domain.StopCancelled, domain.HookCancelled
				case "uncertain":
					status, runCode, runOrigin = domain.HookUncertain, domain.HookOutcomeUncertain, "recovery"
					disposition, code, origin = domain.StopUncertain, domain.HookOutcomeUncertain, domain.HookOriginRecovery
				}
				finished := testStopFinished(t, p, 0, status, runCode, runOrigin)
				results = append(results, finished)
				draft, err := NewHookExecutionFinishedDraft(testTurnID, finished)
				if err != nil {
					t.Fatal(err)
				}
				prefix = append(prefix, draft)
			}
			decision := testStopDecisionDraft(t, p, disposition, code, origin, source, results)
			prefix = append(prefix, decision)
			if disposition == domain.StopPassed {
				prefix = append(prefix, mustDraft(t, EventTurnCompleted, testTurnID, TurnCompletedPayload{}))
			} else {
				prefix = append(prefix, mustDraft(t, EventTurnFailed, testTurnID, TurnFailedPayload{Code: string(code), Cancelled: disposition == domain.StopCancelled}))
			}
			f.appendBatch(prefix...)
			plan, err := NewReplayPlanner().Plan(f.loaded())
			if err != nil || len(plan.Stops) != 1 || len(plan.Turns) != 1 || plan.StopRecovery != nil || len(plan.NativeCommits) != 2 {
				t.Fatal("terminal rejected", err)
			}
			if mode == "cancelled-after-block" && plan.Stops[0].Finished[0].Result.Status != domain.HookBlock {
				t.Fatal("actual result reclassified")
			}
		})
	}
}

func TestStopReplayAllowsCompactionOnlyBeforeDelivery(t *testing.T) {
	for _, stage := range []string{"checking", "pending", "delivered"} {
		t.Run(stage, func(t *testing.T) {
			p := testStopReplayPlan(t, 0, 0, domain.HookFeedbackModel)
			f := testStopReplayFixture(t, p)
			if stage != "checking" {
				testAppendStopBlock(t, f, p)
			}
			if stage == "delivered" {
				f.appendBatch(mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()))
			}
			revision := uint64(2)
			if stage == "delivered" {
				revision++
			}
			source, err := provider.NewCompactionSource(domain.ProviderOpenAI, "responses", "fixture-model", revision, revision, provider.RootCompactionPredecessor(), domain.HookDigest(nil))
			if err != nil {
				t.Fatal(err)
			}
			scope, _ := domain.NewAutoCompactionScope(testTurnID, 1)
			draft, err := NewCompactionStartedDraft(scope, "01900000-0000-7000-8000-000000000009", source)
			if err != nil {
				t.Fatal(err)
			}
			f.appendBatch(draft)
			plan, err := NewReplayPlanner().Plan(f.loaded())
			if stage == "pending" {
				if err != nil || plan.InterruptedCompaction == nil || plan.StopRecovery == nil || !plan.StopRecovery.State().PendingFeedback() {
					t.Fatal("pending boundary rejected", err)
				}
			} else if err == nil {
				t.Fatal("unsafe compaction accepted")
			}
		})
	}
}
