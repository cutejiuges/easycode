package runtime

import (
	"context"
	"errors"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

func stopDeliveryDrafts(prepared *provider.PreparedStopContinuation) ([]session.RecordDraft, error) {
	input, err := prepared.Input()
	if err != nil {
		return nil, err
	}
	envelope, err := prepared.FeedbackEnvelope()
	if err != nil {
		return nil, err
	}
	feedback, err := session.NewProviderNativeCommitDraft(feedbackSampleTurn(input.Feedback()), session.NativeCommitPayload{Provider: envelope.Family(), Wire: envelope.Wire(), Payload: envelope.Payload()})
	if err != nil {
		return nil, err
	}
	drafts := []session.RecordDraft{feedback}
	if a, ok := input.Association(); ok {
		envelope, ok, err := prepared.SteeringEnvelope()
		if err != nil || !ok {
			return nil, domain.HookConfigInvalid
		}
		steering, err := inputCommitDrafts(a, envelope)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, steering...)
	}
	return drafts, session.ValidateDraftBatchSize(drafts)
}

func discardStopCandidate(prepared *provider.PreparedStopContinuation) {
	if prepared != nil && !prepared.Finalized() {
		_ = prepared.Discard()
	}
}

// deliverStopContinuation 在已接受的意图之后只领取一次输入；取消只能去掉未接纳的真实输入。
func (r *Runtime) deliverStopContinuation(ctx context.Context, emit Emitter, check *stopCheck, control turnInputControl) error {
	feedback, ok := check.state.Decision.Feedback()
	if !ok {
		return domain.HookConfigInvalid
	}
	turn := check.state.Plan.Sample().TurnID
	index := check.state.Plan.Sample().SampleIndex + 1
	preparer, ok := r.conversation.(provider.StopContinuationPreparer)
	if !ok {
		r.poisoned.Store(true)
		return fault.New(fault.CodeStreamProtocol, "stop continuation preparation is unavailable")
	}
	var pending []domain.UserInput
	var priorErr error
	if ctx.Err() == nil && control != nil {
		pending, priorErr = control.claim(ctx, turn, index)
	}
	resolve := func(state protocol.InputState, reason protocol.InputReason) {
		if len(pending) > 0 && control != nil {
			control.resolve(turn, pending, state, reason)
		}
		pending = nil
	}
	feedbackOnly, _ := domain.NewStopFeedbackOnly(feedback)
	candidate := feedbackOnly
	if len(pending) > 0 {
		a, err := inputAssociation(turn, index, pending)
		if err == nil && r.inputEncodingFits(a, pending) {
			candidate, err = domain.NewStopWithSteering(feedback, a, pending)
		}
		if err != nil || candidate.Kind() != domain.StopWithSteering {
			resolve(protocol.InputReturned, protocol.InputReasonPreflight)
			candidate = feedbackOnly
		}
	}
	// 候选从未提交到 native history；丢弃后摘要只看旧窗口，成功后仍重建原冻结值。
	if ctx.Err() == nil && priorErr == nil {
		prepared, err := preparer.PrepareStopContinuation(candidate)
		if err != nil && len(pending) > 0 {
			resolve(protocol.InputReturned, protocol.InputReasonPreflight)
			candidate = feedbackOnly
			prepared, err = preparer.PrepareStopContinuation(candidate)
		}
		if err != nil {
			r.poisoned.Store(true)
			return fault.New(fault.CodeStreamProtocol, "prepare stop delivery failed")
		}
		view, footprint, previewErr := prepared.Preview()
		if err := prepared.Discard(); err != nil {
			r.poisoned.Store(true)
			return err
		}
		if previewErr != nil {
			priorErr = previewErr
		} else {
			base, err := r.conversation.ProjectActiveContext()
			if err != nil {
				priorErr = err
			} else {
				priorErr = r.prepareAutomaticWindow(ctx, turn, index, view, footprint, base.Revision(), emit)
			}
		}
		if r.poisoned.Load() || r.journal.Poisoned() {
			resolve(protocol.InputReturned, protocol.InputReasonFailed)
			return priorErr
		}
	}
	if ctx.Err() != nil || priorErr != nil {
		reason := protocol.InputReasonFailed
		if ctx.Err() != nil {
			reason = protocol.InputReasonCancelled
			priorErr = ctx.Err()
		}
		resolve(protocol.InputReturned, reason)
		candidate = feedbackOnly
	}
	prepared, err := preparer.PrepareStopContinuation(candidate)
	if err != nil {
		r.poisoned.Store(true)
		resolve(protocol.InputReturned, protocol.InputReasonFailed)
		return fault.New(fault.CodeStreamProtocol, "prepare stop delivery failed")
	}
	defer func() { discardStopCandidate(prepared) }()
	preflight := func() error {
		if _, err := stopDeliveryDrafts(prepared); err != nil {
			return err
		}
		view, footprint, err := prepared.Preview()
		if err != nil {
			return err
		}
		sampling, _, err := r.planHistory(view, footprint)
		if err != nil {
			return err
		}
		if err := prepared.CheckRequest(sampling); err != nil {
			return fault.New(fault.CodeProviderRequest, "stop continuation request preflight failed")
		}
		return nil
	}
	if ctx.Err() == nil && priorErr == nil {
		priorErr = preflight()
	}
	if len(pending) > 0 && (priorErr != nil || ctx.Err() != nil) {
		discardStopCandidate(prepared)
		reason := protocol.InputReasonPreflight
		if ctx.Err() != nil {
			reason = protocol.InputReasonCancelled
		}
		resolve(protocol.InputReturned, reason)
		candidate = feedbackOnly
		prepared, err = preparer.PrepareStopContinuation(candidate)
		if err != nil {
			r.poisoned.Store(true)
			return err
		}
		if ctx.Err() == nil {
			priorErr = preflight()
		}
	}
	drafts, err := stopDeliveryDrafts(prepared)
	if err != nil {
		r.poisoned.Store(true)
		return fault.New(fault.CodeStreamProtocol, "stop delivery encoding failed")
	}
	writeCtx := ctx
	if candidate.Kind() == domain.StopFeedbackOnly {
		writeCtx = context.WithoutCancel(ctx)
	}
	records, err := r.journal.AppendBatch(writeCtx, drafts)
	if err != nil {
		discardStopCandidate(prepared)
		if r.journal.Poisoned() {
			r.poisoned.Store(true)
			resolve(protocol.InputUncertain, protocol.InputReasonUncertain)
			return fault.New(fault.CodeSessionWrite, "stop delivery outcome is uncertain")
		}
		if candidate.Kind() == domain.StopWithSteering && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			resolve(protocol.InputReturned, protocol.InputReasonCancelled)
			prepared, err = preparer.PrepareStopContinuation(feedbackOnly)
			if err != nil {
				r.poisoned.Store(true)
				return err
			}
			drafts, err = stopDeliveryDrafts(prepared)
			if err == nil {
				records, err = r.journal.AppendBatch(context.WithoutCancel(ctx), drafts)
			}
			priorErr = ctx.Err()
		}
		if err != nil {
			r.poisoned.Store(true)
			resolve(protocol.InputReturned, protocol.InputReasonFailed)
			return fault.New(fault.CodeSessionWrite, "persist stop delivery failed")
		}
	}
	if err := prepared.Finalize(); err != nil {
		r.poisoned.Store(true)
		resolve(protocol.InputUncertain, protocol.InputReasonUncertain)
		return fault.New(fault.CodeSessionWrite, "finalize stop delivery failed")
	}
	var nativeSequences []uint64
	for _, record := range records {
		if record.EventKind == session.EventProviderNativeCommit {
			nativeSequences = append(nativeSequences, record.Sequence)
		}
	}
	if r.stopHistory != nil {
		if err := r.stopHistory.StopFeedbackCommitted(nativeSequences); err != nil {
			r.poisoned.Store(true)
			resolve(protocol.InputUncertain, protocol.InputReasonUncertain)
			return err
		}
	}
	for _, input := range pending {
		if attachment, ok := input.SkillAttachment(); ok {
			r.explicitSkills = append(r.explicitSkills, attachment)
		}
		event, err := protocol.NewUserInputCommitted(input)
		if err != nil {
			r.poisoned.Store(true)
			resolve(protocol.InputUncertain, protocol.InputReasonUncertain)
			return err
		}
		emit(r.decorate(event, turn))
	}
	r.emitTranscriptCommit(turn, records[len(records)-1].Sequence)
	resolve(protocol.InputCommitted, "")
	check.state.DeliverySequence = records[0].Sequence
	check.state.DetailSequence = records[len(records)-1].Sequence
	if err := r.publishStop(turn, check.state, protocol.StopHookDelivered); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return priorErr
}
