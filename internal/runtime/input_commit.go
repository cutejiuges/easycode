package runtime

import (
	"context"
	"errors"
	"fmt"

	"easycode/internal/codec"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tool"
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
	native, err := session.NewProviderNativeCommitDraft(a.TurnID(), session.NativeCommitPayload{Provider: envelope.Family(), Wire: envelope.Wire(), Payload: envelope.Payload()})
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
	if _, err := runtime.prepareInputEvents(a.TurnID(), inputs); err != nil {
		return false
	}
	encoder := runtime.providerOperations.inputEncoding
	if encoder == nil {
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
	preparer := runtime.providerOperations.userInputs
	if preparer == nil {
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
	sampling, _, err := runtime.planHistory(history, footprint)
	if err != nil {
		return rejected(err)
	}
	if err := prepared.CheckRequest(sampling); err != nil {
		return rejected(fault.New(fault.CodeProviderRequest, "input request preflight failed"))
	}
	events, err := runtime.prepareInputEvents(turnID, inputs)
	if err != nil {
		return rejected(fault.New(fault.CodeInvalidInput, "input view encoding failed"))
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
	for i, input := range inputs {
		if a, ok := input.SkillAttachment(); ok {
			runtime.explicitSkills = append(runtime.explicitSkills, a)
		}
		emit(events[i])
	}
	runtime.emitTranscriptCommit(turnID, records[1].Sequence)
	resolve(protocol.InputCommitted, "")
	return inputCommitResult{committed: true}
}

// prepareInputEvents 在 writer 接收前冻结并验证最终展示事件，提交后只发布已验证值。
func (runtime *Runtime) prepareInputEvents(turnID domain.TurnID, inputs []domain.UserInput) ([]protocol.Event, error) {
	if runtime.userInputCommitted == nil {
		return nil, fmt.Errorf("input event factory is unavailable")
	}
	events := make([]protocol.Event, 0, len(inputs))
	for _, input := range inputs {
		event, err := runtime.userInputCommitted(input)
		if err != nil {
			return nil, err
		}
		if event.Kind != protocol.EventUserInputCommitted {
			return nil, fmt.Errorf("input event kind mismatch")
		}
		event.Payload = append([]byte(nil), event.Payload...)
		event = runtime.decorate(event, turnID)
		if err := event.Validate(); err != nil {
			return nil, err
		}
		payload, err := protocol.DecodeUserInputCommitted(event)
		if err != nil {
			return nil, err
		}
		summary, err := domain.NewFileSummary(input.FileAttachments())
		if err != nil {
			return nil, err
		}
		skill, hasSkill := input.SkillAttachment()
		if payload.InputID != input.ID() || payload.Text != input.Text() || !payload.Files.Equal(summary) || (payload.Skill != nil) != hasSkill || hasSkill && *payload.Skill != skill {
			return nil, fmt.Errorf("input event does not match frozen input")
		}
		if _, err := codec.MarshalCanonical(event, session.MaxRecordBytes); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (runtime *Runtime) planHistory(view domain.ActiveContextView, footprint domain.NativeHistoryFootprint) (provider.TurnInput, contextplan.ContextPlan, error) {
	plan, err := runtime.planWindow(view, footprint)
	if err != nil {
		return provider.TurnInput{}, contextplan.ContextPlan{}, fault.New(fault.CodeTurnFailed, "context planning failed")
	}
	if plan.Decision().State() == contextplan.BudgetOverLimit {
		total, _ := plan.TotalEstimate().Tokens()
		limit, _ := plan.Decision().EffectiveLimit()
		return provider.TurnInput{}, contextplan.ContextPlan{}, fault.New(fault.CodeContextLimitExceeded, fmt.Sprintf("estimated context tokens %d exceed effective input limit %d", total, limit))
	}
	sampling, err := runtime.samplingInput()
	return sampling, plan, err
}

// samplingInput 读取当前操作冻结的普通采样来源。
func (runtime *Runtime) samplingInput() (provider.TurnInput, error) {
	return runtime.samplingInputFor(runtime.operationProfile)
}

func (runtime *Runtime) toolCatalogFor(mode domain.WorkMode) tool.CatalogSnapshot {
	if mode == domain.WorkModePlan {
		return runtime.planToolCatalog
	}
	return runtime.toolCatalog
}

func (runtime *Runtime) samplingInputFor(profile contextplan.ProviderProfile) (provider.TurnInput, error) {
	sampling, err := provider.NewSamplingInput(profile.InferenceSettings())
	if err != nil {
		return provider.TurnInput{}, err
	}
	sampling, err = sampling.WithProjectInstructions(runtime.projectInstructions)
	if err != nil {
		return provider.TurnInput{}, err
	}
	mode := profile.WorkMode()
	catalog := runtime.toolCatalogFor(mode)
	if mode == domain.WorkModePlan {
		sampling, err = sampling.WithPlanMode(catalog)
	} else {
		sampling, err = sampling.WithToolCatalog(catalog)
	}
	if err != nil {
		return provider.TurnInput{}, err
	}
	return sampling.WithSkillCatalog(runtime.skillCatalog)
}
