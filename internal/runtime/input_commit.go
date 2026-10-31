package runtime

import (
	"context"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
	"errors"
	"fmt"
)

// turnInputControl 由 loop owner 串行决定领取和最终输入归属。
type turnInputControl interface {
	claim(context.Context, domain.TurnID, uint32) ([]domain.UserInput, error)
	resolve(domain.TurnID, []domain.UserInput, protocol.InputState, protocol.InputReason)
}

type inputCommitResult struct {
	committed         bool
	preflightRejected bool
	err               error
}

func inputAssociation(turnID domain.TurnID, index uint32, inputs []domain.UserInput) (domain.InputAssociation, error) {
	ids := make([]domain.InputID, len(inputs))
	for i, input := range inputs {
		ids[i] = input.ID()
	}
	placement := domain.InputInitial
	if index > 0 {
		placement = domain.InputSteering
	}
	return domain.NewInputAssociation(turnID, placement, index, ids)
}

func inputCommitDrafts(a domain.InputAssociation, envelope provider.NativeCommitEnvelope) ([]session.RecordDraft, error) {
	fact, err := session.NewTurnInputsCommittedDraft(a)
	if err != nil {
		return nil, err
	}
	native, err := session.NewProviderNativeCommitDraft(a.TurnID(), session.NativeCommitPayload{Provider: envelope.Family(), Wire: envelope.Wire(), PayloadVersion: envelope.PayloadVersion(), Payload: envelope.Payload()})
	if err != nil {
		return nil, err
	}
	drafts := []session.RecordDraft{fact, native}
	if err := session.ValidateDraftBatchSize(drafts); err != nil {
		return nil, err
	}
	return drafts, nil
}

func (runtime *Runtime) inputEncodingFits(a domain.InputAssociation, inputs []domain.UserInput) bool {
	encoder, ok := runtime.conversation.(provider.UserInputEncoder)
	if !ok {
		return false
	}
	envelope, err := encoder.EncodeUserInputs(a, inputs)
	if err != nil {
		return false
	}
	_, err = inputCommitDrafts(a, envelope)
	return err == nil
}

func (runtime *Runtime) commitInputs(ctx context.Context, turnID domain.TurnID, index uint32, inputs []domain.UserInput, control turnInputControl, emit Emitter) inputCommitResult {
	resolve := func(state protocol.InputState, reason protocol.InputReason) {
		if control != nil {
			control.resolve(turnID, inputs, state, reason)
		}
	}
	rejected := func(err error) inputCommitResult {
		resolve(protocol.InputReturned, protocol.InputReasonPreflight)
		return inputCommitResult{preflightRejected: true, err: err}
	}
	if ctx.Err() != nil {
		resolve(protocol.InputReturned, protocol.InputReasonCancelled)
		return inputCommitResult{err: ctx.Err()}
	}
	a, err := inputAssociation(turnID, index, inputs)
	if err != nil {
		return rejected(err)
	}
	preparer, ok := runtime.conversation.(provider.UserInputPreparer)
	if !ok {
		return rejected(fmt.Errorf("provider user input preparation is unavailable"))
	}
	prepared, err := preparer.PrepareUserInputs(a, inputs)
	if err != nil {
		return rejected(fault.New(fault.CodeTurnFailed, "user input preparation failed"))
	}
	defer func() {
		if !prepared.Finalized() {
			_ = prepared.Discard()
		}
	}()
	envelope, err := prepared.Envelope()
	if err != nil {
		return rejected(err)
	}
	drafts, err := inputCommitDrafts(a, envelope)
	if err != nil {
		return rejected(fault.New(fault.CodeTurnFailed, "input encoding limit exceeded"))
	}
	history, footprint, err := prepared.Preview()
	if err != nil {
		return rejected(err)
	}
	sampling, err := runtime.planHistory(history, footprint)
	if err != nil {
		return rejected(err)
	}
	if err := prepared.CheckRequest(sampling); err != nil {
		return rejected(fault.New(fault.CodeProviderRequest, "input request preflight failed"))
	}
	records, err := runtime.journal.AppendBatch(ctx, drafts)
	if err != nil {
		if runtime.journal.Poisoned() {
			runtime.poisoned.Store(true)
			resolve(protocol.InputUncertain, protocol.InputReasonUncertain)
		} else if errors.Is(err, context.Canceled) {
			resolve(protocol.InputReturned, protocol.InputReasonCancelled)
		} else {
			resolve(protocol.InputReturned, protocol.InputReasonFailed)
		}
		return inputCommitResult{err: fault.Wrap(fault.CodeSessionWrite, "persist user input failed", err)}
	}
	if err := prepared.Finalize(); err != nil {
		runtime.poisoned.Store(true)
		resolve(protocol.InputUncertain, protocol.InputReasonUncertain)
		return inputCommitResult{err: fault.New(fault.CodeSessionWrite, "finalize user input failed")}
	}
	if runtime.toolHistory != nil {
		if err := runtime.toolHistory.NativeCommitted(records[1].Sequence, nil); err != nil {
			runtime.poisoned.Store(true)
			resolve(protocol.InputUncertain, protocol.InputReasonUncertain)
			return inputCommitResult{err: err}
		}
	}
	for _, input := range inputs {
		event, err := protocol.NewUserInputCommitted(input)
		if err != nil {
			runtime.poisoned.Store(true)
			resolve(protocol.InputUncertain, protocol.InputReasonUncertain)
			return inputCommitResult{err: err}
		}
		emit(runtime.decorate(event, turnID))
	}
	runtime.emitTranscriptCommit(turnID, records[1].Sequence)
	resolve(protocol.InputCommitted, "")
	return inputCommitResult{committed: true}
}

func (runtime *Runtime) planHistory(view domain.ActiveContextView, footprint domain.NativeHistoryFootprint) (provider.TurnInput, error) {
	input, err := contextplan.NewPlanningInput(runtime.profile, runtime.toolCatalog, runtime.projectInstructions, view, footprint, contextplan.NewSamplingInput(), runtime.budget)
	if err != nil {
		return provider.TurnInput{}, fault.New(fault.CodeTurnFailed, "context planning input is invalid")
	}
	plan, err := runtime.planner.Plan(input)
	if err != nil {
		return provider.TurnInput{}, fault.New(fault.CodeTurnFailed, "context planning failed")
	}
	if plan.Decision().State() == contextplan.BudgetOverLimit {
		total, _ := plan.TotalEstimate().Tokens()
		limit, _ := plan.Decision().EffectiveLimit()
		return provider.TurnInput{}, fault.New(fault.CodeContextLimitExceeded, fmt.Sprintf("estimated context tokens %d exceed effective input limit %d", total, limit))
	}
	sampling, err := provider.NewSamplingInput().WithProjectInstructions(runtime.projectInstructions)
	if err != nil {
		return provider.TurnInput{}, err
	}
	return sampling.WithToolCatalog(runtime.toolCatalog)
}
