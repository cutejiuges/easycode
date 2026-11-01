package runtime

import (
	"context"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
)

// prepareAutomaticSample 在唯一 turn worker 内冻结候选并最多尝试一次摘要。
func (runtime *Runtime) prepareAutomaticSample(ctx context.Context, turn domain.TurnID, index uint32, inputs []domain.UserInput, emit Emitter) error {
	if _, enabled := runtime.autoCompactionPolicy.Threshold(); !enabled {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	view, err := runtime.conversation.ProjectActiveContext()
	if err != nil {
		return err
	}
	revision := view.Revision()
	if runtime.autoCompactionFailures >= 3 || (runtime.autoCompactionFailures > 0 && runtime.autoCompactionFailedRevision == revision) {
		return nil
	}
	footprint, err := runtime.conversation.HistoryFootprint()
	if err != nil {
		return err
	}
	if len(inputs) > 0 {
		association, err := inputAssociation(turn, index, inputs)
		if err != nil {
			return nil
		}
		if !runtime.inputEncodingFits(association, inputs) {
			return nil
		}
		preparer, ok := runtime.conversation.(provider.UserInputPreparer)
		if !ok {
			return nil
		}
		prepared, err := preparer.PrepareUserInputs(association, inputs)
		if err != nil {
			return nil
		}
		candidate, candidateFootprint, previewErr := prepared.Preview()
		if err := prepared.Discard(); err != nil {
			runtime.poisoned.Store(true)
			return fault.New(fault.CodeStreamProtocol, "discard input candidate failed")
		}
		if previewErr != nil {
			return previewErr
		}
		view, footprint = candidate, candidateFootprint
	}
	plan, err := runtime.planWindow(view, footprint)
	if err != nil {
		return err
	}
	decision, err := runtime.autoCompactionPolicy.Decide(plan.TotalEstimate(), false)
	if err != nil {
		return err
	}
	if decision != contextplan.AutoCompactionEligible {
		return nil
	}
	admission, code := runtime.preflightCompaction("")
	if code == protocol.ControlErrorNothingToCompact {
		return nil
	}
	if code != "" {
		if code != protocol.ControlErrorCompactionBudgetExceeded && code != protocol.ControlErrorCompactionBudgetUnknown {
			return fault.New(fault.CodeStreamProtocol, "automatic compaction preflight failed")
		}
		runtime.recordAutoCompactionFailure(revision)
		return nil
	}
	id, err := domain.GenerateCompactionID()
	if err != nil {
		return err
	}
	scope, err := domain.NewAutoCompactionScope(turn, index)
	if err != nil {
		return err
	}
	scopedInput, err := admission.preview.Input().WithScope(scope)
	if err != nil {
		return err
	}
	admission.preview, err = provider.NewCompactionPreview(admission.preview.Source(), scopedInput, admission.preview.Estimate())
	if err != nil {
		return err
	}
	failedDurable := false
	failureRecorded := false
	observed := func(event protocol.Event) {
		if event.Kind == protocol.EventCompactionFailed {
			runtime.recordAutoCompactionFailure(revision)
			failureRecorded = true
			payload, err := protocol.DecodeCompactionFailed(event)
			failedDurable = err == nil && payload.Durable && payload.Code != protocol.CompactionFailureSession
		}
		emit(event)
	}
	err = runtime.runCompactionOwned(ctx, id, admission, scope, observed)
	if err == nil {
		return ctx.Err()
	}
	if !failureRecorded {
		runtime.recordAutoCompactionFailure(revision)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if runtime.poisoned.Load() || runtime.journal.Poisoned() || !failedDurable {
		return err
	}
	// 可恢复失败后仍由普通请求的预算与编码预检决定是否继续。
	return nil
}

func (runtime *Runtime) recordAutoCompactionFailure(revision uint64) {
	runtime.autoCompactionFailedRevision = revision
	if runtime.autoCompactionFailures < 3 {
		runtime.autoCompactionFailures++
	}
}
