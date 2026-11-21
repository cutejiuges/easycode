package runtime

import (
	"context"
	"errors"
	"time"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

func (runtime *Runtime) contextPlanFor(profile contextplan.ProviderProfile) (contextplan.ContextPlan, error) {
	plan, _, err := runtime.contextPlanWithRevision(profile)
	return plan, err
}
func (runtime *Runtime) contextPlanWithRevision(profile contextplan.ProviderProfile) (contextplan.ContextPlan, uint64, error) {
	view, footprint, err := runtime.conversation.ProjectContextWindow()
	if err != nil {
		return contextplan.ContextPlan{}, 0, err
	}
	plan, err := runtime.planWindowFor(profile, view, footprint)
	return plan, view.Revision(), err
}
func (runtime *Runtime) planWindow(view domain.ActiveContextView, footprint domain.NativeHistoryFootprint) (contextplan.ContextPlan, error) {
	return runtime.planWindowFor(runtime.operationProfile, view, footprint)
}
func (runtime *Runtime) planWindowFor(profile contextplan.ProviderProfile, view domain.ActiveContextView, footprint domain.NativeHistoryFootprint) (contextplan.ContextPlan, error) {
	input, err := contextplan.NewPlanningInput(profile, runtime.toolCatalogFor(profile.WorkMode()), runtime.projectInstructions, view, footprint, contextplan.NewSamplingInput(), runtime.budget)
	if err != nil {
		return contextplan.ContextPlan{}, err
	}
	input, err = input.WithSkillCatalog(runtime.skillCatalog)
	if err != nil {
		return contextplan.ContextPlan{}, err
	}
	input, err = input.WithTaskProgress(runtime.TaskProgress())
	if err != nil {
		return contextplan.ContextPlan{}, err
	}
	return runtime.planner.Plan(input)
}
func (runtime *Runtime) contextSnapshot() (protocol.ContextMetricsPayload, error) {
	return runtime.contextSnapshotFor(runtime.operationProfile)
}
func (runtime *Runtime) contextSnapshotFor(profile contextplan.ProviderProfile) (protocol.ContextMetricsPayload, error) {
	plan, revision, err := runtime.contextPlanWithRevision(profile)
	if err != nil {
		return protocol.ContextMetricsPayload{}, err
	}
	total, err := protocol.NewContextEstimatePayload(plan.TotalEstimate())
	if err != nil {
		return protocol.ContextMetricsPayload{}, err
	}
	suppressed := runtime.autoCompactionFailures >= 3 || (runtime.autoCompactionFailures > 0 && runtime.autoCompactionFailedRevision == revision)
	decision, err := runtime.autoCompactionPolicy.Decide(plan.TotalEstimate(), suppressed)
	if err != nil {
		return protocol.ContextMetricsPayload{}, err
	}
	policy, err := protocol.NewCachePolicyPayload(runtime.cachePolicy)
	if err != nil {
		return protocol.ContextMetricsPayload{}, err
	}
	snapshot := protocol.ContextMetricsPayload{CachePolicy: policy, AutoCompactionState: string(decision), AutoCompactionReason: runtime.autoCompactionPolicy.UnavailableReason(), Scope: "committed", Revision: revision, Activity: "idle", Model: profile.Model(), Sources: make([]protocol.ContextSourcePayload, 0), Total: total, WindowConfigured: runtime.budget.Enabled(), Window: runtime.budget.Window(), ReservedOutput: runtime.budget.ReservedOutput(), SafetyMargin: runtime.budget.SafetyMargin(), BudgetState: string(plan.Decision().State())}
	if threshold, enabled := runtime.autoCompactionPolicy.Threshold(); enabled {
		snapshot.AutoCompactionThreshold = &threshold
	}
	for _, source := range plan.Sources() {
		estimate, err := protocol.NewContextEstimatePayload(source.Estimate())
		if err != nil {
			return protocol.ContextMetricsPayload{}, err
		}
		snapshot.Sources = append(snapshot.Sources, protocol.ContextSourcePayload{Kind: string(source.Kind()), Revision: source.Revision(), Estimate: estimate})
	}
	return snapshot, snapshot.Validate()
}

type compactionAdmission struct {
	profile contextplan.ProviderProfile
	kernel  provider.CompactionKernel
	preview provider.CompactionPreview
	before  contextplan.ContextPlan
}

func (runtime *Runtime) preflightCompaction(instructions string) (compactionAdmission, protocol.ControlErrorCode) {
	return runtime.preflightCompactionFor(runtime.operationProfile, instructions)
}
func (runtime *Runtime) preflightCompactionFor(profile contextplan.ProviderProfile, instructions string) (compactionAdmission, protocol.ControlErrorCode) {
	kernel := runtime.providerOperations.compaction
	if kernel == nil {
		return compactionAdmission{}, protocol.ControlErrorInvalidCommand
	}
	// 摘要按实际摘要 wire 预算，普通候选窗口仍使用本次捕获的操作策略。
	summaryProfile, err := profile.WithWorkMode(domain.WorkModeDefault)
	if err != nil {
		return compactionAdmission{}, protocol.ControlErrorInvalidCommand
	}
	sampling, err := runtime.samplingInputFor(summaryProfile)
	if err != nil {
		return compactionAdmission{}, protocol.ControlErrorInvalidCommand
	}
	input, err := provider.NewCompactionInput(sampling, instructions, runtime.compactionOutputTokens)
	if err != nil {
		return compactionAdmission{}, protocol.ControlErrorInvalidCommand
	}
	input, err = input.WithTaskProgress(runtime.TaskProgress())
	if err != nil {
		return compactionAdmission{}, protocol.ControlErrorInvalidCommand
	}
	preview, projectWindow, err := kernel.PreflightCompaction(input)
	if err != nil {
		var failure *provider.CompactionError
		if errors.As(err, &failure) {
			switch failure.Code() {
			case provider.CompactionNothing:
				return compactionAdmission{}, protocol.ControlErrorNothingToCompact
			case provider.CompactionBudgetExceeded:
				return compactionAdmission{}, protocol.ControlErrorCompactionBudgetExceeded
			case provider.CompactionBusy:
				return compactionAdmission{}, protocol.ControlErrorSessionBusy
			}
		}
		return compactionAdmission{}, protocol.ControlErrorInvalidCommand
	}
	if err := contextplan.CheckCompactionRequest(preview.Estimate(), input.OutputTokens(), runtime.budget); err != nil {
		var rejection *contextplan.CompactionBudgetError
		if errors.As(err, &rejection) && rejection.Reason() == contextplan.CompactionBudgetUnknown {
			return compactionAdmission{}, protocol.ControlErrorCompactionBudgetUnknown
		}
		return compactionAdmission{}, protocol.ControlErrorCompactionBudgetExceeded
	}
	view, footprint, err := projectWindow()
	if err != nil {
		return compactionAdmission{}, protocol.ControlErrorInvalidCommand
	}
	before, err := runtime.planWindowFor(profile, view, footprint)
	if err != nil {
		return compactionAdmission{}, protocol.ControlErrorInvalidCommand
	}
	return compactionAdmission{profile: profile, kernel: kernel, preview: preview, before: before}, ""
}

func (runtime *Runtime) runCompaction(ctx context.Context, id domain.CompactionID, admission compactionAdmission, emit Emitter) (resultErr error) {
	runtime.markSkillsStarted()
	if !runtime.active.CompareAndSwap(false, true) {
		return runtime.failCompaction(id, protocol.CompactionFailureSession, false, nil, emit)
	}
	defer runtime.active.Store(false)
	runtime.operationProfile = admission.profile
	defer func() { runtime.operationProfile = runtime.profile }()
	defer func() { runtime.releaseFailedContinuation(ctx, resultErr) }()
	_, err := runtime.runCompactionOwned(ctx, id, admission, domain.ManualCompactionScope(), emit)
	return err
}

func (runtime *Runtime) runCompactionOwned(ctx context.Context, id domain.CompactionID, admission compactionAdmission, scope domain.CompactionScope, emit Emitter) (bool, error) {
	checkpointCommitted := false
	fail := func(code protocol.CompactionFailureCode, started bool, prepared *provider.PreparedCompaction) (bool, error) {
		if scope.Trigger == domain.CompactionAuto && !checkpointCommitted {
			runtime.recordAutoCompactionFailure(admission.preview.Source().ContextRevision())
		}
		return checkpointCommitted, runtime.failScopedCompaction(scope, id, code, started, prepared, emit)
	}
	check, rejection, err := runtime.planPreCompact(id, scope, admission.preview)
	if err != nil {
		runtime.poisoned.Store(true)
		return fail(protocol.CompactionFailureSession, false, nil)
	}
	started, err := session.NewCompactionStartedDraft(scope, id, admission.preview.Source(), check.plan)
	if err != nil {
		runtime.poisoned.Store(true)
		return fail(protocol.CompactionFailureSession, false, nil)
	}
	if _, err = runtime.commitHookFacts(ctx, []session.RecordDraft{started}); err != nil {
		code := compactionFailureCode(ctx, err)
		if runtime.poisoned.Load() {
			code = protocol.CompactionFailureSession
		}
		return fail(code, false, nil)
	}
	event, err := protocol.NewCompactionStarted(runtime.sessionID, runtime.threadID, id, admission.preview.Source().ContextRevision(), scope)
	if err != nil {
		runtime.poisoned.Store(true)
		return fail(protocol.CompactionFailureSession, false, nil)
	}
	emit(event)
	if rejection != "" {
		return fail(rejection, true, nil)
	}
	if len(check.declarations) != 0 {
		outcome := runtime.runPreCompact(ctx, id, scope, admission, check, emit)
		if outcome.code != "" {
			// 已观察到的清理不确定先收口失败，再永久关闭当前运行时的接纳。
			committed, err := fail(outcome.code, true, nil)
			if outcome.code == protocol.CompactionFailureSession {
				runtime.poisoned.Store(true)
			}
			return committed, err
		}
		admission.preview = outcome.preview
	}
	if ctx.Err() != nil {
		return fail(compactionFailureCode(ctx, ctx.Err()), true, nil)
	}
	runtime.cache.frozen, err = contextplan.NewCacheSources(admission.before)
	if err != nil {
		return fail(protocol.CompactionFailureSession, true, nil)
	}
	if runtime.interactiveApprovals {
		event, err := protocol.NewCompactionSummarizing(runtime.sessionID, runtime.threadID, id, scope)
		if err != nil {
			runtime.poisoned.Store(true)
			return fail(protocol.CompactionFailureSession, false, nil)
		}
		emit(event)
	}
	if ctx.Err() != nil {
		return fail(compactionFailureCode(ctx, ctx.Err()), true, nil)
	}
	networkCtx, cancel := runtime.compactionDeadline(ctx, 120*time.Second)
	prepared, err := admission.kernel.RunCompaction(networkCtx, id, admission.preview)
	code := compactionFailureCode(networkCtx, err)
	cancel()
	if err != nil {
		var failure *provider.CompactionError
		if errors.As(err, &failure) && (failure.Code() == provider.CompactionBusy || failure.Code() == provider.CompactionSourceChanged) {
			runtime.poisoned.Store(true)
			code = protocol.CompactionFailureSession
		}
		return fail(code, true, nil)
	}
	view, footprint, err := prepared.Preview()
	if err != nil {
		runtime.poisoned.Store(true)
		return fail(protocol.CompactionFailureSession, true, prepared)
	}
	candidate, err := runtime.planWindow(view, footprint)
	if err == nil {
		err = contextplan.CheckCompactionCandidate(admission.before, candidate)
	}
	if err != nil {
		return fail(compactionFailureCode(ctx, err), true, prepared)
	}
	checkpoint, err := prepared.Checkpoint()
	if err != nil || checkpoint.ID() != id || checkpoint.Source() != admission.preview.Source() || checkpoint.Scope() != scope {
		runtime.poisoned.Store(true)
		return fail(protocol.CompactionFailureResponse, true, prepared)
	}
	usage, err := prepared.Usage()
	if err != nil {
		runtime.poisoned.Store(true)
		return fail(protocol.CompactionFailureSession, true, prepared)
	}
	receipt, receiptErr := prepared.Receipt()
	if receiptErr != nil {
		return fail(protocol.CompactionFailureResponse, true, prepared)
	}
	observation, observationErr := runtime.cacheObservation(receipt, usage, domain.RequestSummary, id)
	if observationErr != nil {
		return fail(protocol.CompactionFailureResponse, true, prepared)
	}
	post, err := runtime.planPostCompact(checkpoint)
	if err != nil {
		runtime.poisoned.Store(true)
		return fail(protocol.CompactionFailureSession, false, prepared)
	}
	completed, err := post.completionDraft(checkpoint, usage, admission.before.TotalEstimate(), candidate.TotalEstimate())
	if err != nil {
		if session.IsRecordSizeLimit(err) {
			return fail(protocol.CompactionFailureBudget, true, prepared)
		}
		runtime.poisoned.Store(true)
		return fail(protocol.CompactionFailureSession, false, prepared)
	}
	records, err := runtime.journal.AppendBatch(ctx, []session.RecordDraft{completed})
	if err != nil {
		if runtime.journal.Poisoned() {
			runtime.poisoned.Store(true)
			return fail(protocol.CompactionFailureSession, false, prepared)
		}
		return fail(compactionFailureCode(ctx, err), true, prepared)
	}
	checkpointCommitted = true
	// 成功 Sync 优先于取消。窗口安装失败保留成功记录，交由下次恢复。
	if err = prepared.Finalize(); err != nil {
		runtime.poisoned.Store(true)
		return fail(protocol.CompactionFailureSession, false, nil)
	}
	summary, ok := view.Summary()
	if len(records) != 1 || records[0].Sequence == 0 || !ok ||
		runtime.compactionHistory != nil && runtime.compactionHistory.CompactionCommitted(records[0].Sequence, id, summary) != nil {
		runtime.poisoned.Store(true)
		return fail(protocol.CompactionFailureSession, false, nil)
	}
	runtime.cache.snapshot.LastSummary = &observation
	runtime.cache.snapshot.LastFailure = ""
	runtime.autoCompactionFailures = 0
	runtime.autoCompactionFailedRevision = 0
	event, err = protocol.NewCompactionCompleted(runtime.sessionID, runtime.threadID, id, view.Revision(), admission.before.TotalEstimate(), candidate.TotalEstimate(), usage, scope)
	if err != nil {
		runtime.poisoned.Store(true)
		return fail(protocol.CompactionFailureSession, false, nil)
	}
	var postErr error
	if runtime.interactiveApprovals && post.plan.Kind() != session.PostCompactPlanNone {
		target, err := domain.NewCompactionHookTarget(id, scope)
		snapshot, snapshotErr := runtime.contextSnapshot()
		if err != nil || snapshotErr != nil || snapshot.Revision != view.Revision() {
			runtime.poisoned.Store(true)
			return fail(protocol.CompactionFailureSession, false, nil)
		}
		postErr = runtime.runPostCompact(ctx, target, summary, records[0].Sequence, snapshot, post, emit)
	}
	emit(event)
	return checkpointCommitted, postErr
}

func (runtime *Runtime) failCompaction(id domain.CompactionID, code protocol.CompactionFailureCode, started bool, prepared *provider.PreparedCompaction, emit Emitter) error {
	return runtime.failScopedCompaction(domain.ManualCompactionScope(), id, code, started, prepared, emit)
}

func (runtime *Runtime) failScopedCompaction(scope domain.CompactionScope, id domain.CompactionID, code protocol.CompactionFailureCode, started bool, prepared *provider.PreparedCompaction, emit Emitter) error {
	payload := session.CompactionFailedPayload{Scope: scope, ID: id, Code: code, UsageState: "unavailable"}
	eventPayload := protocol.CompactionFailedPayload{Scope: scope, Code: code, UsageState: "unavailable"}
	if prepared != nil {
		usage, usageErr := prepared.Usage()
		provenance, provenanceErr := prepared.UsageProvenance()
		if usageErr == nil && provenanceErr == nil {
			value, err := session.NewSampleUsagePayload(usage)
			if err == nil {
				payload.UsageState = "observed"
				payload.Usage = &value
				payload.Provenance = &session.NativeCommitPayload{Provider: provenance.Family(), Wire: provenance.Wire(), Payload: provenance.Payload()}
				// 公共事件只接收归一化计量，绝不复制 Provider raw usage。
				usagePayload, err := protocol.NewUsagePayload(usage)
				if err == nil {
					eventPayload.UsageState = "observed"
					eventPayload.Usage = &usagePayload
				}

			}
		}
		if err := prepared.Discard(); err != nil {
			runtime.poisoned.Store(true)
		}
	}
	if started && !runtime.poisoned.Load() && !runtime.journal.Poisoned() {
		draft, err := session.NewCompactionFailedDraft(payload)
		if err == nil {
			_, err = runtime.journal.AppendBatch(context.Background(), []session.RecordDraft{draft})
		}
		if err == nil {
			eventPayload.Durable = true
		} else {
			runtime.poisoned.Store(true)
			code = protocol.CompactionFailureSession
			eventPayload.Code = code
		}
	}
	event, err := protocol.NewCompactionFailed(runtime.sessionID, runtime.threadID, id, eventPayload)
	if err == nil {
		emit(event)
	} else {
		runtime.poisoned.Store(true)
	}
	return fault.New(fault.CodeTurnFailed, string(code))
}

func compactionFailureCode(ctx context.Context, err error) protocol.CompactionFailureCode {
	if ctx.Err() == context.DeadlineExceeded {
		return protocol.CompactionFailureTimeout
	}
	if ctx.Err() != nil {
		return protocol.CompactionFailureCancelled
	}
	var budget *contextplan.CompactionBudgetError
	if errors.As(err, &budget) {
		switch budget.Reason() {
		case contextplan.CompactionNoReduction:
			return protocol.CompactionFailureNoReduction
		case contextplan.CompactionBudgetUnknown:
			return protocol.CompactionFailureUnknownBudget
		default:
			return protocol.CompactionFailureBudget
		}
	}
	var failure *provider.CompactionError
	if errors.As(err, &failure) {
		switch failure.Code() {
		case provider.CompactionCancelled:
			return protocol.CompactionFailureCancelled
		case provider.CompactionTimedOut:
			return protocol.CompactionFailureTimeout
		case provider.CompactionInvalidResponse, provider.CompactionSourceChanged:
			return protocol.CompactionFailureResponse
		}
	}
	return protocol.CompactionFailureRequest
}
