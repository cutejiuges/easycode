package runtime

import (
	"context"
	"errors"
	"time"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/session"
)

type promptSubmitRejection struct {
	code domain.HookCode
	ref  domain.HookReference
}

type promptSubmitCheck struct {
	hookExecutionConfig
	plan         session.PromptSubmitPlan
	declarations []domain.HookDeclaration
}

// checkPromptSubmissions 在真实输入可影响上下文之前完成检查和 claim 筛选。
func (r *Runtime) checkPromptSubmissions(ctx context.Context, emit Emitter, turn domain.TurnID, index uint32, inputs []domain.UserInput, control turnInputControl, remaining *time.Duration) ([]domain.UserInput, domain.HookCode, error) {
	if len(inputs) == 0 {
		return inputs, "", nil
	}
	snapshot, execution := r.captureHookExecution()
	declarations, matchErr := snapshot.UserPromptSubmitChecks()
	if matchErr == nil && len(declarations) == 0 {
		return inputs, "", nil
	}

	rejected := make([]promptSubmitRejection, len(inputs))
	err := ctx.Err()
	if err == nil {
		if matchErr != nil {
			code := domain.HookConfigInvalid
			if errors.Is(matchErr, domain.HookMatchLimit) {
				code = domain.HookMatchLimit
			}
			for i := range rejected {
				rejected[i].code = code
			}
		} else {
			err = r.runPromptSubmitCheck(ctx, emit, turn, index, inputs, rejected, remaining, execution, declarations)
		}
	}

	var firstRejected domain.HookCode
	passed := make([]domain.UserInput, 0, len(inputs))
	for i, input := range inputs {
		if code := rejected[i].code; code == "" {
			passed = append(passed, input)
		} else if firstRejected == "" {
			firstRejected = code
		}
	}
	if control != nil && firstRejected != "" && !control.filter(turn, inputs, rejected) {
		return nil, "", r.promptFactsFailure()
	}
	if err != nil {
		if control != nil && len(passed) > 0 {
			reason := protocol.InputReasonFailed
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				reason = protocol.InputReasonCancelled
			}
			control.resolve(turn, passed, protocol.InputReturned, reason)
		}
		return nil, firstRejected, err
	}
	return passed, firstRejected, nil
}

func (r *Runtime) runPromptSubmitCheck(ctx context.Context, emit Emitter, turn domain.TurnID, index uint32, inputs []domain.UserInput, rejected []promptSubmitRejection, remaining *time.Duration, execution hookExecutionConfig, declarations []domain.HookDeclaration) error {
	association, err := inputAssociation(turn, index, inputs)
	if err != nil {
		return r.promptFactsFailure()
	}
	plan, err := session.NewPromptSubmitPlan(association, declarations)
	if err != nil {
		return r.promptFactsFailure()
	}
	draft, err := session.NewPromptSubmitPlannedDraft(plan)
	if err := r.appendPromptFact(ctx, draft, err); err != nil {
		return err
	}

	check := promptSubmitCheck{hookExecutionConfig: execution, plan: plan, declarations: declarations}
	settlement := context.WithoutCancel(ctx)
	availability := r.promptSubmitAvailability(ctx, execution)
	state := session.PromptSubmitCompleted
	var stopErr error
	for i, input := range inputs {
		if ctx.Err() != nil {
			state, stopErr = session.PromptSubmitCancelled, ctx.Err()
			break
		}
		verdict := promptSubmitRejection{code: availability}
		if availability == domain.HookOK {
			verdict, err = r.checkPromptInput(ctx, emit, check, check.plan.Target(input.ID()), input.Text(), remaining)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				state, stopErr = session.PromptSubmitCancelled, err
				break
			}
			if err != nil {
				if verdict.code != domain.HookOutcomeUncertain {
					return err
				}
				rejected[i] = verdict
				state, stopErr = session.PromptSubmitUncertain, err
				break
			}
		}
		draft, buildErr := session.NewPromptSubmitDecidedDraft(plan, input.ID(), verdict.code)
		if err := r.appendPromptFact(settlement, draft, buildErr); err != nil {
			return err
		}
		if verdict.code != domain.HookOK {
			rejected[i] = verdict
		}
	}
	if state == session.PromptSubmitCompleted && ctx.Err() != nil {
		state, stopErr = session.PromptSubmitCancelled, ctx.Err()
	}
	draft, err = session.NewPromptSubmitClosedDraft(plan, state)
	if err := r.appendPromptFact(settlement, draft, err); err != nil {
		return err
	}
	return stopErr
}

func (r *Runtime) promptSubmitAvailability(ctx context.Context, execution hookExecutionConfig) domain.HookCode {
	if !r.interactiveApprovals {
		return domain.HookTrustRequired
	}
	if execution.availability != domain.HookOK {
		return execution.availability
	}
	return execution.runner.Preflight(ctx)
}

func (r *Runtime) checkPromptInput(ctx context.Context, emit Emitter, check promptSubmitCheck, promptTarget domain.PromptSubmitTarget, text string, remaining *time.Duration) (promptSubmitRejection, error) {
	if *remaining <= 0 {
		return promptSubmitRejection{code: domain.HookExecutionBudgetExhausted}, nil
	}
	input, err := domain.NewUserPromptSubmitHookInput(r.sessionID, r.threadID, promptTarget, check.workspace, text)
	if err != nil {
		if errors.Is(err, domain.HookInputLimit) {
			return promptSubmitRejection{code: domain.HookInputLimit}, nil
		}
		return promptSubmitRejection{}, r.promptFactsFailure()
	}
	target, err := domain.NewUserPromptSubmitHookTarget(promptTarget)
	if err != nil {
		return promptSubmitRejection{}, r.promptFactsFailure()
	}
	for ordinal, declaration := range check.declarations {
		if ctx.Err() != nil {
			return promptSubmitRejection{}, ctx.Err()
		}
		if *remaining <= 0 {
			return promptSubmitRejection{code: domain.HookExecutionBudgetExhausted}, nil
		}
		code, err := r.trustHook(ctx, emit, promptTarget.TurnID, target, check.plan.Digest(), declaration)
		if ctx.Err() != nil {
			return promptSubmitRejection{}, ctx.Err()
		}
		if err != nil {
			return promptSubmitRejection{}, r.promptFactsFailure()
		}
		if code != domain.HookOK {
			return promptSubmitRejection{code: code, ref: declaration.Reference}, nil
		}
		result, err := r.runPromptSubmitDeclaration(ctx, emit, check, target, input, promptTarget.TurnID, ordinal, declaration, remaining)
		if err != nil {
			return promptSubmitRejection{}, err
		}
		if result.Status == domain.HookUncertain {
			if !result.CleanupConfirmed {
				r.poisoned.Store(true)
			}
			return promptSubmitRejection{code: result.Code, ref: declaration.Reference}, promptSubmitFailure(result.Code)
		}
		if result.Status == domain.HookCancel {
			return promptSubmitRejection{}, context.Canceled
		}
		if result.Status != domain.HookContinue {
			return promptSubmitRejection{code: result.Code, ref: declaration.Reference}, nil
		}
	}
	return promptSubmitRejection{code: domain.HookOK}, nil
}

func (r *Runtime) runPromptSubmitDeclaration(ctx context.Context, emit Emitter, check promptSubmitCheck, target domain.HookTarget, input domain.HookInput, turn domain.TurnID, ordinal int, declaration domain.HookDeclaration, remaining *time.Duration) (domain.HookRunResult, error) {
	id, err := domain.NewUserPromptSubmitHookRunIdentity(target, check.plan.Digest(), ordinal)
	if err != nil {
		return domain.HookRunResult{}, r.promptFactsFailure()
	}
	started, err := session.NewHookExecutionStartedDraft(turn, id)
	if err := r.appendPromptFact(ctx, started, err); err != nil {
		return domain.HookRunResult{}, err
	}
	activity := protocol.HookActivityPayload{Target: target, PlanDigest: check.plan.Digest(), Reference: declaration.Reference, State: protocol.HookActivityChecking, Code: domain.HookOK}
	r.emitPromptSubmitActivity(emit, turn, activity)
	execution, left := runTimedHook(ctx, check.runner, check.now, *remaining, id, declaration, input)
	*remaining = left
	result := execution.Result
	if !validHookExecution(execution, id, domain.HookFeedbackNone) {
		result = domain.HookRunResult{Identity: id, Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain, Origin: "execution"}
	}
	finished, err := domain.NewEmptyHookFinished(result, declaration.Reference)
	if err != nil {
		return domain.HookRunResult{}, r.promptFactsFailure()
	}
	draft, err := session.NewHookExecutionFinishedDraft(turn, finished)
	if err := r.appendPromptFact(context.WithoutCancel(ctx), draft, err); err != nil {
		return domain.HookRunResult{}, err
	}
	activity.State, activity.Code = protocol.HookActivityState(result.Status), result.Code
	activity.StdoutBytes, activity.StderrBytes = result.StdoutBytes, result.StderrBytes
	r.emitPromptSubmitActivity(emit, turn, activity)
	return result, nil
}

func (r *Runtime) emitPromptSubmitActivity(emit Emitter, turn domain.TurnID, activity protocol.HookActivityPayload) {
	if event, err := protocol.NewHookActivity(activity); err == nil {
		emit(r.decorate(event, turn))
	}
}

func (r *Runtime) appendPromptFact(ctx context.Context, draft session.RecordDraft, err error) error {
	if err != nil {
		return r.promptFactsFailure()
	}
	_, err = r.commitHookFacts(ctx, []session.RecordDraft{draft})
	return err
}

func (r *Runtime) promptFactsFailure() error {
	r.poisoned.Store(true)
	return fault.New(fault.CodeSessionWrite, "prompt check facts could not be settled")
}

func promptSubmitFailure(code domain.HookCode) error {
	return fault.New(fault.Code(code), "user prompt check did not allow this input")
}
