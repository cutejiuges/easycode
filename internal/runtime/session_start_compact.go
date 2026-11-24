package runtime

import (
	"context"
	"errors"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

func (r *Runtime) runCompactSessionStart(ctx context.Context, target domain.SessionStartTarget, admission compactionAdmission, emit Emitter) (resultErr error) {
	selection := r.selectSessionStart(domain.SessionStartCompact)
	if len(selection.declarations) == 0 && selection.skip == "" {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	terminal := false
	observed := func(event protocol.Event) {
		if event.Kind == protocol.EventSessionStart {
			payload, err := protocol.DecodeSessionStart(event)
			terminal = err == nil && payload.State != protocol.SessionStartPlanned
		}
		emit(event)
	}
	attempt, err := r.runSessionStart(ctx, target, selection, observed)
	defer func() {
		if resultErr != nil && attempt.admitted && !terminal {
			_ = r.publishSessionStart(observed, protocol.SessionStartPayload{Target: target, PlanDigest: attempt.plan.Digest(),
				State: protocol.SessionStartUncertain, Code: domain.HookOutcomeUncertain, Count: attempt.finished})
		}
	}()
	if err != nil {
		return err
	}
	if attempt.state != session.SessionStartApplied {
		return r.closeSessionStart(ctx, observed, attempt)
	}
	return r.commitCompactSessionStart(ctx, admission, observed, attempt)
}

func (r *Runtime) commitCompactSessionStart(ctx context.Context, admission compactionAdmission, emit Emitter, attempt sessionStartAttempt) (resultErr error) {
	if ctx.Err() != nil {
		attempt.cancel()
		return r.closeSessionStart(ctx, emit, attempt)
	}
	preparer := r.providerOperations.sessionStart
	if preparer == nil {
		return r.sessionStartFailure()
	}
	prepared, err := preparer.PrepareCompactSessionStartContext(attempt.candidate, admission.next)
	if err != nil {
		if sessionStartBudgetRejection(err) {
			attempt.skip(domain.HookFeedbackBudgetExceeded)
			return r.closeSessionStart(ctx, emit, attempt)
		}
		return r.sessionStartFailure()
	}
	defer func() {
		if !prepared.Finalized() {
			if err := prepared.Discard(); err != nil {
				resultErr = r.sessionStartFailure()
			}
		}
	}()
	draft, revision, err := r.prepareCompactSessionStartDraft(prepared, admission, attempt)
	if sessionStartBudgetRejection(err) {
		attempt.skip(domain.HookFeedbackBudgetExceeded)
		if ctx.Err() != nil {
			attempt.cancel()
		}
		return r.closeSessionStart(ctx, emit, attempt)
	}
	if err != nil {
		return r.sessionStartFailure()
	}
	records, err := r.commitHookFacts(ctx, []session.RecordDraft{draft})
	if err != nil {
		if !r.poisoned.Load() && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			attempt.cancel()
			return r.closeSessionStart(ctx, emit, attempt)
		}
		return err
	}
	if err := prepared.Finalize(); err != nil || len(records) != 1 {
		return r.sessionStartFailure()
	}
	if err := r.sessionStartCommitted(emit, attempt, []uint64{records[0].Sequence}, revision); err != nil {
		return err
	}
	return ctx.Err()
}

func (r *Runtime) prepareCompactSessionStartDraft(prepared *provider.PreparedSessionStartContext, admission compactionAdmission, attempt sessionStartAttempt) (session.RecordDraft, uint64, error) {
	var draft session.RecordDraft
	view, footprint, err := prepared.Preview()
	if err != nil {
		return draft, 0, err
	}
	plan, err := r.planWindow(view, footprint)
	if err != nil {
		return draft, 0, err
	}
	if err := contextplan.CheckCompactionCandidate(admission.before, plan); err != nil {
		return draft, 0, err
	}
	next, nextFootprint, err := prepared.ContinuationPreview()
	if err != nil {
		return draft, 0, err
	}
	sampling, _, err := r.planHistory(next, nextFootprint)
	if err != nil {
		return draft, 0, err
	}
	if err := prepared.CheckRequest(sampling); err != nil {
		return draft, 0, err
	}
	envelope, err := prepared.Envelope()
	if err != nil {
		return draft, 0, err
	}
	draft, err = attempt.closeDraft(envelope)
	if err == nil {
		err = session.ValidateSingletonDraftSize(draft)
	}
	return draft, view.Revision(), err
}
