package runtime

import (
	"context"
	"errors"
	"time"

	"easycode/internal/codec"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

// SessionStartHistoryObserver 安装同一原生提交批次的真实游标。
type SessionStartHistoryObserver interface {
	SessionStartCommitted([]uint64) error
}

// InstallSessionStartSource 在宿主接管前登记待处理来源，执行仍由实际首轮接纳。
func (r *Runtime) InstallSessionStartSource(source domain.SessionStartSource) error {
	if r.active.Load() || r.sessionStartSource != "" || (source != domain.SessionStartStartup && source != domain.SessionStartResume) {
		return fault.New(fault.CodeInvalidConfiguration, "invalid session start activation")
	}
	r.sessionStartSource = source
	return nil
}

type sessionStartSelection struct {
	compactionHookExecution
	declarations []domain.HookDeclaration
	skip         domain.HookCode
}

func (r *Runtime) selectSessionStart(source domain.SessionStartSource) sessionStartSelection {
	if !r.interactiveApprovals {
		return sessionStartSelection{}
	}
	snapshot, execution := r.captureCompactionHooks()
	declarations, err := snapshot.SessionStartChecks(source)
	selection := sessionStartSelection{compactionHookExecution: execution, declarations: declarations}
	if err != nil {
		selection.skip = domain.HookConfigInvalid
		if errors.Is(err, domain.HookMatchLimit) {
			selection.skip = domain.HookMatchLimit
		}
	}
	return selection
}

type sessionStartAttempt struct {
	plan      session.SessionStartPlan
	admitted  bool
	finished  int
	state     session.SessionStartCloseState
	code      domain.HookCode
	candidate domain.SessionStartContext
}

func (a *sessionStartAttempt) skip(code domain.HookCode) {
	a.state, a.code = session.SessionStartSkipped, code
	a.candidate.Items = nil
}

func (a *sessionStartAttempt) cancel() {
	a.state, a.code = session.SessionStartCancelled, domain.HookCancelled
	a.candidate.Items = nil
}

func (r *Runtime) publishSessionStart(emit Emitter, payload protocol.SessionStartPayload) error {
	event, err := protocol.NewSessionStart(r.sessionID, r.threadID, payload)
	if err != nil {
		return r.sessionStartFailure()
	}
	emit(event)
	return nil
}

func (r *Runtime) sessionStartFailure() error {
	r.poisoned.Store(true)
	return fault.New(fault.CodeSessionWrite, "session start context could not be safely committed")
}

func (r *Runtime) runSessionStart(ctx context.Context, target domain.SessionStartTarget, selection sessionStartSelection, emit Emitter) (sessionStartAttempt, error) {
	a := sessionStartAttempt{state: session.SessionStartNoContext, code: domain.HookOK, candidate: domain.SessionStartContext{Target: target}}
	if ctx.Err() != nil {
		return a, ctx.Err()
	}
	if selection.skip != "" {
		return a, r.publishSessionStart(emit, protocol.SessionStartPayload{Target: target, State: protocol.SessionStartSkipped, Code: selection.skip})
	}
	plan, err := session.NewSessionStartPlan(target, selection.declarations)
	if err != nil {
		return a, r.sessionStartFailure()
	}
	draft, err := session.NewSessionStartHookPlannedDraft(plan)
	if err != nil {
		return a, r.sessionStartFailure()
	}
	if _, err := r.commitHookFacts(ctx, []session.RecordDraft{draft}); err != nil {
		return a, err
	}
	a.plan, a.admitted = plan, true
	if err := r.publishSessionStart(emit, protocol.SessionStartPayload{Target: target, PlanDigest: plan.Digest(), State: protocol.SessionStartPlanned, Code: domain.HookOK, Count: len(selection.declarations)}); err != nil {
		return a, err
	}
	if err := r.checkSessionStart(ctx, emit, selection, &a); err != nil {
		return a, err
	}
	if ctx.Err() != nil && a.state != session.SessionStartUncertain {
		a.cancel()
	}
	return a, nil
}

func (r *Runtime) checkSessionStart(ctx context.Context, emit Emitter, selection sessionStartSelection, attempt *sessionStartAttempt) error {
	target := attempt.plan.Target()
	if selection.availability != domain.HookOK {
		attempt.skip(selection.availability)
		return nil
	}
	if code := selection.runner.Preflight(ctx); code != domain.HookOK {
		attempt.skip(code)
		return nil
	}
	input, err := domain.NewSessionStartHookInput(r.sessionID, r.threadID, target, selection.workspace)
	if err != nil {
		attempt.skip(domain.HookInputLimit)
		return nil
	}
	hookTarget, err := domain.NewSessionStartHookTarget(target)
	if err != nil {
		return r.sessionStartFailure()
	}
	turn := target.TurnID
	if target.Source == domain.SessionStartCompact {
		turn = target.Scope.TurnID
	}
	for _, declaration := range selection.declarations {
		if ctx.Err() != nil {
			return nil
		}
		code, err := r.trustHook(ctx, emit, turn, hookTarget, attempt.plan.Digest(), declaration)
		if err != nil {
			return r.sessionStartFailure()
		}
		if code != domain.HookOK {
			attempt.skip(code)
			return nil
		}
	}

	remaining := 30 * time.Second
	var failure domain.HookCode
	bytes := 0
	full := false
	for ordinal, declaration := range selection.declarations {
		if ctx.Err() != nil {
			return nil
		}
		if remaining <= 0 {
			attempt.code = domain.HookExecutionBudgetExhausted
			break
		}
		identity, err := domain.NewSessionStartHookRunIdentity(hookTarget, attempt.plan.Digest(), ordinal)
		if err != nil {
			return r.sessionStartFailure()
		}
		started, err := session.NewSessionStartHookStartedDraft(identity)
		if err != nil {
			return r.sessionStartFailure()
		}
		activity, err := protocol.NewHookActivity(protocol.HookActivityPayload{Target: hookTarget, PlanDigest: identity.PlanDigest, Reference: declaration.Reference, State: protocol.HookActivityChecking, Code: domain.HookOK})
		if err != nil {
			return r.sessionStartFailure()
		}
		if _, err := r.commitHookFacts(ctx, []session.RecordDraft{started}); err != nil {
			if ctx.Err() != nil && !r.poisoned.Load() {
				return nil
			}
			return err
		}
		emit(r.decorate(activity, turn))
		execution, left := runTimedHook(ctx, selection.runner, selection.now, remaining, identity, declaration, input)
		remaining = left
		result := execution.Result
		var proof *domain.SessionStartProof
		if !validHookExecution(execution, identity, declaration.Reference.Feedback) {
			result = domain.HookRunResult{Identity: identity, Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain, Origin: "execution"}
		} else if !execution.Feedback.Empty() {
			body, err := domain.WrapSessionStartContext(target.Source, execution.Feedback.Content())
			if err != nil {
				return r.sessionStartFailure()
			}
			item := domain.SessionStartContextItem{Ordinal: ordinal, Reference: declaration.Reference, Text: body}
			value := item.Proof()
			proof = &value
			full = full || bytes+value.Bytes > domain.MaxSessionStartContextBytes
			if !full {
				attempt.candidate.Items = append(attempt.candidate.Items, item)
				bytes += value.Bytes
			}
		}
		finished, err := session.NewSessionStartHookFinishedDraft(result, proof)
		if err != nil {
			return r.sessionStartFailure()
		}
		if _, err := r.commitHookFacts(context.WithoutCancel(ctx), []session.RecordDraft{finished}); err != nil {
			return err
		}
		attempt.finished++
		switch result.Status {
		case domain.HookUncertain:
			attempt.state, attempt.code = session.SessionStartUncertain, domain.HookOutcomeUncertain
			attempt.candidate.Items = nil
			return nil
		case domain.HookCancel:
			attempt.cancel()
			return nil
		case domain.HookError:
			if failure == "" {
				failure = result.Code
			}
		}
	}
	if len(attempt.candidate.Items) > 0 {
		attempt.state = session.SessionStartApplied
	} else if attempt.code != domain.HookOK {
		attempt.skip(attempt.code)
	} else if full {
		attempt.skip(domain.HookFeedbackBudgetExceeded)
	} else if failure != "" {
		attempt.skip(failure)
	}
	return nil
}

func (attempt sessionStartAttempt) closeDraft(envelope provider.NativeCommitEnvelope) (session.RecordDraft, error) {
	var proofs []domain.SessionStartProof
	for _, item := range attempt.candidate.Items {
		proofs = append(proofs, item.Proof())
	}
	return session.NewSessionStartHookClosedDraft(attempt.plan, attempt.finished, attempt.state, attempt.code, proofs, envelope)
}

func (r *Runtime) closeSessionStart(ctx context.Context, emit Emitter, attempt sessionStartAttempt) error {
	if !attempt.admitted {
		return ctx.Err()
	}
	draft, err := attempt.closeDraft(provider.NativeCommitEnvelope{})
	if err != nil {
		return r.sessionStartFailure()
	}
	records, err := r.commitHookFacts(context.WithoutCancel(ctx), []session.RecordDraft{draft})
	if err != nil {
		return err
	}
	state := protocol.SessionStartSkipped
	switch attempt.state {
	case session.SessionStartUncertain:
		state = protocol.SessionStartUncertain
	case session.SessionStartCancelled:
		state = protocol.SessionStartCancelled
	}
	if err := r.publishSessionStart(emit, protocol.SessionStartPayload{Target: attempt.plan.Target(), PlanDigest: attempt.plan.Digest(), State: state, Code: attempt.code, Count: attempt.finished, Sequence: records[0].Sequence}); err != nil {
		return err
	}
	if state == protocol.SessionStartUncertain {
		return r.sessionStartFailure()
	}
	if state == protocol.SessionStartCancelled && ctx.Err() == nil {
		return context.Canceled
	}
	return ctx.Err()
}

func sessionStartBudgetRejection(err error) bool {
	var budget *contextplan.CompactionBudgetError
	return errors.Is(err, fault.New(fault.CodeContextLimitExceeded, "")) || codec.IsCanonicalSizeLimit(err) ||
		session.IsRecordSizeLimit(err) || session.IsBatchSizeLimit(err) ||
		errors.As(err, &budget) && (budget.Reason() == contextplan.CompactionNoReduction || budget.Reason() == contextplan.CompactionBudgetExceeded)
}

func (r *Runtime) sessionStartCommitted(emit Emitter, attempt sessionStartAttempt, sequences []uint64, revision uint64) error {
	if r.sessionStartHistory != nil {
		if err := r.sessionStartHistory.SessionStartCommitted(sequences); err != nil {
			return r.sessionStartFailure()
		}
	}
	return r.publishSessionStart(emit, protocol.SessionStartPayload{Target: attempt.plan.Target(), PlanDigest: attempt.plan.Digest(), State: protocol.SessionStartApplied,
		Code: attempt.code, Count: len(attempt.candidate.Items), Sequence: sequences[0], Revision: revision})
}
