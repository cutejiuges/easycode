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

// stopTurnBudget 只属于一个 turn worker，工具及续轮都不能重置它。
type stopTurnBudget struct {
	continuations uint8
	feedbackBytes int
	remaining     time.Duration
}

type stopCheck struct {
	state        session.ReplayedStop
	declarations []domain.HookDeclaration
	runner       HookRunner
	workspace    string
	availability domain.HookCode
	now          func() time.Time
}

func (r *Runtime) completionCheck(sample domain.AssistantSampleReference, budget stopTurnBudget) (*stopCheck, error) {
	r.hooks.mu.Lock()
	snapshot, runner, workspace, availability, now := r.hooks.snapshot.Clone(), r.hooks.runner, r.hooks.workspace, r.hooks.availability, r.hooks.now
	r.hooks.mu.Unlock()
	if runner == nil {
		return nil, nil
	}
	ds, matchErr := snapshot.StopChecks()
	if matchErr == nil && len(ds) == 0 {
		return nil, nil
	}
	var plan domain.StopPlan
	var err error
	if matchErr != nil {
		code := domain.HookConfigInvalid
		if errors.Is(matchErr, domain.HookMatchLimit) {
			code = domain.HookMatchLimit
		}
		plan, err = domain.NewUnavailableStopPlan(sample, budget.continuations, code)
	} else {
		refs := make([]domain.HookReference, len(ds))
		for i, d := range ds {
			refs[i] = d.Reference
		}
		plan, err = domain.NewStopChecksPlan(sample, budget.continuations, refs)
	}
	if err != nil {
		return nil, err
	}
	return &stopCheck{state: session.ReplayedStop{Plan: plan}, declarations: ds, runner: runner, workspace: workspace, availability: availability, now: now}, nil
}

func (r *Runtime) commitStopSample(ctx context.Context, turn domain.TurnID, sample *provider.PreparedSample, check *stopCheck) error {
	defer discardPreparedSample(sample)
	observation, err := r.sampleObservation(sample)
	if err != nil {
		return err
	}
	envelope, err := sample.Envelope()
	if err != nil {
		return err
	}
	usage, err := sample.Usage()
	if err != nil {
		return err
	}
	native, err := session.NewProviderNativeCommitDraft(turn, session.NativeCommitPayload{Provider: envelope.Family(), Wire: envelope.Wire(), Payload: envelope.Payload()})
	if err != nil {
		return err
	}
	used, err := session.NewSampleUsageDraft(turn, usage)
	if err != nil {
		return err
	}
	planned, err := session.NewTurnStopHookPlannedDraft(check.state.Plan)
	if err != nil {
		return err
	}
	records, err := r.journal.AppendBatch(context.WithoutCancel(ctx), []session.RecordDraft{native, used, planned})
	if err != nil {
		r.sampleWriteFailed()
		r.poisoned.Store(true)
		return fault.New(fault.CodeSessionWrite, "persist completion sample failed")
	}
	if err := r.sampleDurable(records[0].Sequence); err != nil {
		r.poisoned.Store(true)
		return err
	}
	if err := sample.Finalize(); err != nil {
		r.poisoned.Store(true)
		return fault.New(fault.CodeStreamProtocol, "finalize completion sample failed")
	}
	r.commitSampleObservation(observation)
	if r.toolHistory != nil {
		if err := r.toolHistory.NativeCommitted(records[0].Sequence, nil); err != nil {
			r.poisoned.Store(true)
			return err
		}
	}
	r.emitSampleCommit(turn, records[0].Sequence)
	check.state.PlanSequence, check.state.DetailSequence = records[2].Sequence, records[2].Sequence
	return r.publishStop(turn, check.state, protocol.StopHookPlanned)
}

// decideStop 同一批次保存实际结果和决定；只有最终成功才产生聚合完成事件。
func (r *Runtime) decideStop(ctx context.Context, emit Emitter, check *stopCheck, disposition domain.StopDisposition, code domain.HookCode, origin domain.HookOrigin, source domain.StopDecisionSource, prefix []session.RecordDraft, feedback domain.StopFeedback, usages []domain.SampleUsage) (bool, error) {
	p := check.state.Plan
	d, err := domain.NewStopDecision(p, disposition, code, origin, source, check.state.Finished, feedback)
	if err != nil {
		return false, err
	}
	draft, err := session.NewTurnStopHookDecidedDraft(d)
	if err != nil {
		return false, err
	}
	drafts := append(prefix, draft)
	var terminal protocol.Event
	var failure error
	if disposition == domain.StopPassed {
		usage, err := domain.AggregateSampleUsage(usages)
		if err != nil {
			return false, err
		}
		terminal, err = protocol.NewTurnCompleted(usage)
		if err != nil {
			return false, err
		}
		complete, err := session.NewTurnCompletedDraft(p.Sample().TurnID)
		if err != nil {
			return false, err
		}
		drafts = append(drafts, complete)
	} else if disposition != domain.StopContinued {
		cancelled := disposition == domain.StopCancelled
		failed, err := session.NewTurnFailedDraft(p.Sample().TurnID, session.TurnFailedPayload{Code: string(code), Message: "Completion check stopped: " + string(code), Cancelled: cancelled})
		if err != nil {
			return false, err
		}
		drafts = append(drafts, failed)
		terminal, err = protocol.NewTurnFailed(string(code), "Completion check stopped: "+string(code), cancelled)
		if err != nil {
			return false, err
		}
		failure = fault.New(fault.Code(code), "Completion check stopped: "+string(code))
	}
	writeCtx := context.WithoutCancel(ctx)
	if disposition == domain.StopContinued || disposition == domain.StopPassed {
		writeCtx = ctx
	}
	records, err := r.commitHookFacts(writeCtx, drafts)
	if err != nil && !r.poisoned.Load() && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return r.decideStop(context.WithoutCancel(ctx), emit, check, domain.StopCancelled, domain.HookCancelled, origin, source, prefix, domain.StopFeedback{}, usages)
	}
	if err != nil {
		return false, err
	}
	check.state.Started, check.state.StartedSequence = nil, 0
	check.state.Decision = &d
	check.state.DecisionSequence = records[len(prefix)].Sequence
	check.state.DetailSequence = records[len(records)-1].Sequence
	if err := r.publishStop(p.Sample().TurnID, check.state, protocol.StopHookDecided); err != nil {
		return false, err
	}
	if disposition != domain.StopContinued {
		if disposition == domain.StopPassed {
			usage, _ := domain.AggregateSampleUsage(usages)
			payload, _ := protocol.NewUsagePayload(usage)
			r.cache.snapshot.LastTurn = &payload
			r.publishCacheSnapshot()
		}
		emit(r.decorate(terminal, p.Sample().TurnID))
	}
	return disposition == domain.StopContinued, failure
}

func (r *Runtime) runStopCheck(ctx context.Context, emit Emitter, check *stopCheck, budget *stopTurnBudget, usages []domain.SampleUsage) (bool, error) {
	p := check.state.Plan
	finishPlan := func(disposition domain.StopDisposition, code domain.HookCode) (bool, error) {
		return r.decideStop(ctx, emit, check, disposition, code, domain.HookOriginExecution, domain.NewStopPlanSource(), nil, domain.StopFeedback{}, usages)
	}
	if ctx.Err() != nil {
		return finishPlan(domain.StopCancelled, domain.HookCancelled)
	}
	if p.Kind() == domain.StopPlanUnavailable {
		return finishPlan(domain.StopError, p.UnavailableCode())
	}
	if !r.interactiveApprovals {
		return finishPlan(domain.StopBlocked, domain.HookTrustRequired)
	}
	if check.availability != domain.HookOK {
		return finishPlan(domain.StopError, check.availability)
	}
	text, err := r.conversation.ProjectAssistantSample(p.Sample())
	if err != nil {
		return finishPlan(domain.StopError, domain.HookInputLimit)
	}
	stopInput, err := domain.NewStopInput(r.sessionID, r.threadID, p.Sample(), check.workspace, budget.continuations > 0, text.Text())
	if err != nil {
		return finishPlan(domain.StopError, domain.HookInputLimit)
	}
	input, err := domain.NewStopHookInput(stopInput)
	if err != nil {
		return finishPlan(domain.StopError, domain.HookInputLimit)
	}
	if budget.remaining <= 0 {
		return finishPlan(domain.StopError, domain.HookExecutionBudgetExhausted)
	}
	// 全部声明先授权，避免前面的脚本在后面的拒绝之前产生副作用。
	for ordinal, declaration := range check.declarations {
		code, err := r.trustHook(ctx, emit, p.Sample().TurnID, p.Target(), p.Digest(), declaration)
		if ctx.Err() != nil || code == domain.HookCancelled {
			return finishPlan(domain.StopCancelled, domain.HookCancelled)
		}
		if err != nil || code != domain.HookOK {
			source, _ := domain.NewStopHookSource(ordinal)
			return r.decideStop(ctx, emit, check, domain.StopBlocked, domain.HookTrustDenied, domain.HookOriginExecution, source, nil, domain.StopFeedback{}, usages)
		}
	}
	for ordinal, declaration := range check.declarations {
		if ctx.Err() != nil {
			return finishPlan(domain.StopCancelled, domain.HookCancelled)
		}
		if budget.remaining <= 0 {
			return finishPlan(domain.StopError, domain.HookExecutionBudgetExhausted)
		}
		id, _ := domain.NewStopHookRunIdentity(p, ordinal)
		started, err := session.NewHookExecutionStartedDraft(p.Sample().TurnID, id)
		if err != nil {
			return false, err
		}
		records, err := r.commitHookFacts(ctx, []session.RecordDraft{started})
		if err != nil {
			if !r.poisoned.Load() && ctx.Err() != nil {
				return finishPlan(domain.StopCancelled, domain.HookCancelled)
			}
			return false, err
		}
		check.state.Started, check.state.StartedSequence = &id, records[0].Sequence
		check.state.DetailSequence = records[0].Sequence
		if err := r.publishStop(p.Sample().TurnID, check.state, protocol.StopHookStarted); err != nil {
			return false, err
		}
		r.emitHookActivity(p.Sample().TurnID, protocol.HookActivityPayload{Target: p.Target(), PlanDigest: p.Digest(), Reference: declaration.Reference, State: "checking", Code: domain.HookOK})
		start := check.now()
		execution := check.runner.Run(ctx, id, declaration, input, min(budget.remaining, time.Duration(declaration.Reference.TimeoutSeconds)*time.Second))
		budget.remaining -= max(0, check.now().Sub(start))
		result := execution.Result
		if _, err := domain.NewHookExecution(result, declaration.Reference.Feedback, execution.Feedback); err != nil || result.Identity != id || result.Origin != "execution" {
			result = domain.HookRunResult{Identity: id, Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain, Origin: "execution"}
			execution.Feedback = domain.HookFeedbackCandidate{}
		}
		finished, err := domain.NewEmptyHookFinished(result, declaration.Reference)
		if err != nil {
			return false, err
		}
		draft, err := session.NewHookExecutionFinishedDraft(p.Sample().TurnID, finished)
		if err != nil {
			return false, err
		}
		check.state.Finished = append(check.state.Finished, finished)
		check.state.Started, check.state.StartedSequence = nil, 0
		publishResult := func() {
			r.emitHookActivity(p.Sample().TurnID, protocol.HookActivityPayload{Target: p.Target(), PlanDigest: p.Digest(), Reference: declaration.Reference, State: protocol.HookActivityState(result.Status), Code: result.Code, StdoutBytes: result.StdoutBytes, StderrBytes: result.StderrBytes})
		}
		if result.Status == domain.HookContinue && ordinal+1 < len(check.declarations) && ctx.Err() == nil {
			records, err := r.commitHookFacts(context.WithoutCancel(ctx), []session.RecordDraft{draft})
			if err != nil {
				return false, err
			}
			check.state.DetailSequence = records[0].Sequence
			publishResult()
			if err := r.publishStop(p.Sample().TurnID, check.state, protocol.StopHookFinished); err != nil {
				return false, err
			}
			continue
		}
		disposition, code, source := domain.StopPassed, domain.HookOK, domain.NewStopPlanSource()
		var feedback domain.StopFeedback
		if result.Status != domain.HookContinue {
			source, _ = domain.NewStopHookSource(ordinal)
		}
		switch result.Status {
		case domain.HookContinue:
		case domain.HookBlock:
			disposition, code = domain.StopBlocked, domain.HookDenied
			if !execution.Feedback.Empty() {
				switch {
				case budget.continuations >= domain.MaxStopContinuations:
					code = domain.HookStopLimitExceeded
				case p.Sample().SampleIndex+1 >= maxSamplesPerTurn:
					code = domain.HookSampleLimitExceeded
				default:
					feedback, err = domain.NewStopFeedback(result, declaration.Reference, execution.Feedback)
					if err != nil {
						disposition, code = domain.StopError, domain.HookFeedbackBudgetExceeded
					} else if budget.feedbackBytes+feedback.Bytes > domain.MaxTurnStopFeedbackBytes {
						disposition, code = domain.StopError, domain.HookFeedbackBudgetExceeded
					} else if r.preencodeStopFeedback(feedback) != nil {
						disposition, code = domain.StopError, domain.HookFeedbackEncodingFailed
					} else {
						disposition = domain.StopContinued
					}
				}
			}
		case domain.HookUncertain:
			disposition, code = domain.StopUncertain, domain.HookOutcomeUncertain
		case domain.HookCancel:
			disposition, code = domain.StopCancelled, domain.HookCancelled
		default:
			disposition, code = domain.StopError, result.Code
		}
		// 取消不改写已观察到的命令结果，但在接纳 continued 之前关闭交付意图。
		if ctx.Err() != nil && (result.Status == domain.HookContinue || result.Status == domain.HookBlock || result.Status == domain.HookCancel) {
			disposition, code = domain.StopCancelled, domain.HookCancelled
			if result.Status == domain.HookContinue {
				source = domain.NewStopPlanSource()
			}
		}
		if disposition != domain.StopContinued {
			feedback = domain.StopFeedback{}
		}
		continued, err := r.decideStop(ctx, emit, check, disposition, code, domain.HookOriginExecution, source, []session.RecordDraft{draft}, feedback, usages)
		if err == nil || check.state.Decision != nil {
			publishResult()
		}
		if continued {
			budget.continuations++
			budget.feedbackBytes += feedback.Bytes
		}
		return continued, err
	}
	return false, domain.HookConfigInvalid
}

func (r *Runtime) preencodeStopFeedback(feedback domain.StopFeedback) error {
	encoder, ok := r.conversation.(provider.StopFeedbackEncoder)
	if !ok {
		return domain.HookFeedbackEncodingFailed
	}
	envelope, err := encoder.EncodeStopFeedback(feedback)
	if err != nil {
		return err
	}
	draft, err := session.NewProviderNativeCommitDraft(feedbackSampleTurn(feedback), session.NativeCommitPayload{Provider: envelope.Family(), Wire: envelope.Wire(), Payload: envelope.Payload()})
	if err != nil {
		return err
	}
	return session.ValidateDraftBatchSize([]session.RecordDraft{draft})
}

func feedbackSampleTurn(feedback domain.StopFeedback) domain.TurnID {
	sample, _ := feedback.Identity.Target.AssistantSample()
	return sample.TurnID
}
