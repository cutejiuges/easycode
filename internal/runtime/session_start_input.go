package runtime

import (
	"context"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
)

func (r *Runtime) commitInitialInputs(ctx context.Context, turn domain.TurnID, inputs []domain.UserInput, control turnInputControl, emit Emitter) inputCommitResult {
	source := r.sessionStartSource
	if source == "" || ctx.Err() != nil {
		return r.commitInputs(ctx, turn, 0, inputs, control, emit)
	}
	selection := r.selectSessionStart(source)
	target := domain.SessionStartTarget{Source: source, TurnID: turn}
	resolve := resolveInputCommit(control, turn, inputs)
	if len(selection.declarations) == 0 {
		r.sessionStartSource = ""
		if selection.skip != "" {
			if err := r.publishSessionStart(emit, protocol.SessionStartPayload{Target: target, State: protocol.SessionStartSkipped, Code: selection.skip}); err != nil {
				resolve(protocol.InputReturned, protocol.InputReasonFailed)
				return inputCommitResult{err: err}
			}
		}
		return r.commitInputs(ctx, turn, 0, inputs, control, emit)
	}
	plain, err := r.prepareInputCommit(turn, 0, inputs)
	if err != nil {
		resolve(protocol.InputReturned, protocol.InputReasonPreflight)
		return inputCommitResult{preflightRejected: true, err: err}
	}
	if err := plain.discard(); err != nil {
		resolve(protocol.InputReturned, protocol.InputReasonFailed)
		return inputCommitResult{err: r.sessionStartFailure()}
	}
	attempt, err := r.runSessionStart(ctx, target, selection, emit)
	if attempt.admitted {
		r.sessionStartSource = ""
	}
	if err == nil && attempt.state == session.SessionStartApplied {
		var commit preparedInputCommit
		commit, err = r.prepareSessionStartInputs(turn, inputs, plain.events, attempt)
		switch {
		case err == nil:
			return r.commitPreparedInputs(ctx, turn, inputs, resolve, emit, commit, &attempt)
		case sessionStartBudgetRejection(err):
			attempt.skip(domain.HookFeedbackBudgetExceeded)
			err = nil
		default:
			err = r.sessionStartFailure()
		}
	}
	if err == nil {
		err = r.closeSessionStart(ctx, emit, attempt)
	}
	if err != nil {
		reason := protocol.InputReasonFailed
		if ctx.Err() != nil {
			reason = protocol.InputReasonCancelled
		}
		resolve(protocol.InputReturned, reason)
		return inputCommitResult{err: err}
	}
	return r.commitInputs(ctx, turn, 0, inputs, control, emit)
}

func (r *Runtime) prepareSessionStartInputs(turn domain.TurnID, inputs []domain.UserInput, events []protocol.Event, attempt sessionStartAttempt) (commit preparedInputCommit, resultErr error) {
	if r.providerOperations.sessionStart == nil {
		return commit, r.sessionStartFailure()
	}
	a, err := inputAssociation(turn, 0, inputs)
	if err != nil {
		return commit, err
	}
	prepared, err := r.providerOperations.sessionStart.PrepareSessionStartInputs(attempt.candidate, a, inputs)
	if err != nil {
		return commit, err
	}
	commit.change = prepared
	defer func() {
		if resultErr != nil {
			if err := commit.discard(); err != nil {
				resultErr = r.sessionStartFailure()
			}
		}
	}()
	envelope, err := prepared.Envelope()
	if err != nil {
		return commit, err
	}
	input, err := prepared.InputEnvelope()
	if err != nil {
		return commit, err
	}
	closeDraft, err := attempt.closeDraft(envelope)
	if err != nil {
		return commit, err
	}
	inputDrafts, err := inputCommitDrafts(a, input)
	if err != nil {
		return commit, err
	}
	commit.drafts = append([]session.RecordDraft{closeDraft}, inputDrafts...)
	if err := session.ValidateDraftBatchSize(commit.drafts); err != nil {
		return commit, err
	}
	view, footprint, err := prepared.Preview()
	if err != nil {
		return commit, err
	}
	sampling, _, err := r.planHistory(view, footprint)
	if err != nil {
		return commit, err
	}
	if err := prepared.CheckRequest(sampling); err != nil {
		return commit, err
	}
	commit.events, commit.revision = events, view.Revision()
	return commit, nil
}
