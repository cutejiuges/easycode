package runtime

import (
	"context"
	"errors"
	"time"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

type postCompactCheck struct {
	hookExecutionConfig
	plan         session.PostCompactPlan
	declarations []domain.HookDeclaration
}

func (runtime *Runtime) planPostCompact(checkpoint provider.CheckpointEnvelope) (postCompactCheck, error) {
	snapshot, execution := runtime.captureHookExecution()
	check := postCompactCheck{hookExecutionConfig: execution}
	declarations, err := snapshot.PostCompactChecks(checkpoint.Scope().Trigger)
	switch {
	case errors.Is(err, domain.HookMatchLimit):
		check.plan, err = session.NewSkippedPostCompactPlan(domain.HookMatchLimit)
	case err != nil:
		return check, err
	case len(declarations) == 0:
		check.plan = session.NewNoPostCompactPlan()
	case !runtime.interactiveApprovals:
		check.plan, err = session.NewSkippedPostCompactPlan(domain.HookTrustRequired)
	default:
		check.declarations = declarations
		check.plan, err = session.NewPostCompactPlan(checkpoint, declarations)
	}
	return check, err
}

// completionDraft 在 writer 接纳前检查完整信封。只有计划导致的超限允许改用跳过形状。
func (check *postCompactCheck) completionDraft(checkpoint provider.CheckpointEnvelope, usage domain.SampleUsage, before, after domain.TokenEstimate) (session.RecordDraft, error) {
	draft, err := session.NewCompactionCompletedDraft(checkpoint.Scope(), checkpoint, usage, before, after, check.plan)
	if err == nil && check.plan.Kind() != session.PostCompactPlanNone {
		err = session.ValidateSingletonDraftSize(draft)
	}
	if check.plan.Digest() == "" || !session.IsRecordSizeLimit(err) {
		return draft, err
	}
	check.plan, err = session.NewSkippedPostCompactPlan(domain.HookPlanLimit)
	if err != nil {
		return session.RecordDraft{}, err
	}
	check.declarations = nil
	draft, err = session.NewCompactionCompletedDraft(checkpoint.Scope(), checkpoint, usage, before, after, check.plan)
	if err == nil {
		err = session.ValidateSingletonDraftSize(draft)
	}
	return draft, err
}

func (runtime *Runtime) runPostCompact(ctx context.Context, target domain.HookTarget, summary domain.ContextSummary, checkpointSequence uint64, snapshot protocol.ContextMetricsPayload, check postCompactCheck, emit Emitter) error {
	id, scope, _ := target.Compaction()
	installed, err := protocol.NewPostCompactInstalled(runtime.sessionID, runtime.threadID, id, protocol.PostCompactInstalledPayload{
		Scope: scope, CheckpointSequence: checkpointSequence, Revision: snapshot.Revision,
		PlanDigest: check.plan.Digest(), Count: check.plan.Count(), SkipCode: check.plan.Code(),
	})
	if err != nil {
		return runtime.failPostCompact()
	}
	// 交接同一份已验证快照，避免宿主通知重算失败后仍进入审查。
	if runtime.snapshotPublisher != nil {
		runtime.snapshotPublisher(runtimePublication{event: installed, snapshot: snapshot, cacheSnapshot: runtime.cache.snapshot})
	} else {
		emit(installed)
	}
	if check.plan.Digest() == "" {
		return nil
	}

	finished, cause, runErr := runtime.observePostCompact(ctx, target, summary, check, emit)
	status := protocol.PostCompactClosedPayload{
		Scope: scope, CheckpointSequence: checkpointSequence, PlanDigest: check.plan.Digest(),
		State: protocol.PostCompactUncertain, Code: domain.HookOutcomeUncertain, Finished: len(finished),
	}
	if runErr == nil {
		if ctx.Err() != nil {
			cause = domain.HookCancelled
		}
		draft, closed, err := session.NewPostCompactHookClosedDraft(target, check.plan, finished, cause)
		if err == nil {
			var records []session.Record
			records, err = runtime.commitHookFacts(context.WithoutCancel(ctx), []session.RecordDraft{draft})
			if err == nil && len(records) == 1 && records[0].Sequence > checkpointSequence {
				status.State, status.Code, status.CloseSequence = closed.State, closed.Code, records[0].Sequence
			}
		}
	}
	event, err := protocol.NewPostCompactClosed(runtime.sessionID, runtime.threadID, id, status)
	if err != nil {
		return runtime.failPostCompact()
	}
	if status.State == protocol.PostCompactUncertain {
		err = runtime.failPostCompact()
	}
	emit(event)
	return err
}

func (runtime *Runtime) failPostCompact() error {
	runtime.poisoned.Store(true)
	return fault.New(fault.CodeSessionWrite, "post-compaction check could not be safely closed")
}

func (runtime *Runtime) observePostCompact(ctx context.Context, target domain.HookTarget, summary domain.ContextSummary, check postCompactCheck, emit Emitter) ([]domain.HookRunResult, domain.HookCode, error) {
	if ctx.Err() != nil {
		return nil, domain.HookCancelled, nil
	}
	if check.availability != domain.HookOK {
		return nil, check.availability, nil
	}
	if code := check.runner.Preflight(ctx); code != domain.HookOK {
		if ctx.Err() != nil {
			code = domain.HookCancelled
		}
		return nil, code, nil
	}
	id, scope, _ := target.Compaction()
	input, err := domain.NewPostCompactHookInput(runtime.sessionID, runtime.threadID, id, scope, check.workspace, summary)
	if err != nil {
		return nil, domain.HookInputLimit, nil
	}
	for _, declaration := range check.declarations {
		if ctx.Err() != nil {
			return nil, domain.HookCancelled, nil
		}
		code, err := runtime.trustHook(ctx, emit, scope.TurnID, target, check.plan.Digest(), declaration)
		if err != nil || code != domain.HookOK {
			return nil, code, err
		}
	}

	finished := make([]domain.HookRunResult, 0, len(check.declarations))
	remaining := 30 * time.Second
	for ordinal, declaration := range check.declarations {
		if ctx.Err() != nil {
			return finished, domain.HookCancelled, nil
		}
		if remaining <= 0 {
			return finished, domain.HookExecutionBudgetExhausted, nil
		}
		identity, err := domain.NewPostCompactHookRunIdentity(target, check.plan.Digest(), ordinal)
		if err != nil {
			return finished, "", err
		}
		activity, err := protocol.NewHookActivity(protocol.HookActivityPayload{
			Target: target, PlanDigest: check.plan.Digest(), Reference: declaration.Reference, State: protocol.HookActivityChecking, Code: domain.HookOK,
		})
		if err != nil {
			return finished, "", err
		}
		started, err := session.NewPostCompactHookStartedDraft(identity)
		if err != nil {
			return finished, "", err
		}
		if _, err := runtime.commitHookFacts(ctx, []session.RecordDraft{started}); err != nil {
			if ctx.Err() != nil && !runtime.poisoned.Load() {
				return finished, domain.HookCancelled, nil
			}
			return finished, "", err
		}
		emit(runtime.decorate(activity, scope.TurnID))
		execution, left := runTimedHook(ctx, check.runner, check.now, remaining, identity, declaration, input)
		remaining = left
		result := execution.Result
		if !validHookExecution(execution, identity, declaration.Reference.Feedback) {
			result = domain.HookRunResult{Identity: identity, Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain, Origin: "execution"}
		}
		draft, err := session.NewPostCompactHookFinishedDraft(result)
		if err == nil {
			_, err = runtime.commitHookFacts(context.WithoutCancel(ctx), []session.RecordDraft{draft})
		}
		if err != nil {
			return finished, "", err
		}
		finished = append(finished, result)
		if result.Status == domain.HookUncertain || result.Status == domain.HookCancel {
			break
		}
	}
	return finished, domain.HookOK, nil
}
