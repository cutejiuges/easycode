package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

// ReconcileStopTurn 只消费已验证的 durable 事实，不读取当前配置、输入队列或网络。
func (r *Runtime) ReconcileStopTurn(ctx context.Context, plan session.StopRecoveryPlan) error {
	if plan.Validate() != nil || r.poisoned.Load() || r.journal.Poisoned() || !r.active.CompareAndSwap(false, true) {
		return fault.New(fault.CodeSessionCorruption, "invalid stop recovery")
	}
	defer r.active.Store(false)
	state := plan.State()
	check := &stopCheck{state: state}
	if state.Decision == nil {
		disposition, code, source := domain.StopCancelled, domain.HookStopInterrupted, domain.NewStopPlanSource()
		var prefix []session.RecordDraft
		if state.Started != nil {
			disposition, code = domain.StopUncertain, domain.HookOutcomeUncertain
			source, _ = domain.NewStopHookSource(state.Started.Ordinal)
			result := domain.HookRunResult{Identity: *state.Started, Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain, Origin: "recovery"}
			finished, err := domain.NewEmptyHookFinished(result, state.Plan.Checks()[state.Started.Ordinal])
			if err != nil {
				return err
			}
			draft, err := session.NewHookExecutionFinishedDraft(plan.TurnID(), finished)
			if err != nil {
				return err
			}
			check.state.Finished = append(check.state.Finished, finished)
			prefix = append(prefix, draft)
		} else if state.Plan.Kind() == domain.StopPlanUnavailable {
			disposition, code = domain.StopError, state.Plan.UnavailableCode()
		}
		_, err := r.decideStop(ctx, func(protocol.Event) {}, check, disposition, code, domain.HookOriginRecovery, source, prefix, domain.StopFeedback{}, nil)
		if check.state.Decision != nil {
			return nil
		}
		return err
	}
	if state.PendingFeedback() {
		feedback, _ := state.Decision.Feedback()
		candidate, err := domain.NewStopFeedbackOnly(feedback)
		if err != nil {
			return err
		}
		preparer, ok := r.conversation.(provider.StopContinuationPreparer)
		if !ok {
			return fault.New(fault.CodeSessionCorruption, "stop feedback recovery is unavailable")
		}
		prepared, err := preparer.PrepareStopContinuation(candidate)
		if err != nil {
			return fault.New(fault.CodeSessionCorruption, "stop feedback recovery encoding failed")
		}
		defer discardStopCandidate(prepared)
		drafts, err := stopDeliveryDrafts(prepared)
		if err != nil {
			return err
		}
		records, err := r.commitHookFacts(ctx, drafts)
		if err != nil {
			return err
		}
		if err := prepared.Finalize(); err != nil {
			r.poisoned.Store(true)
			return err
		}
		if r.toolHistory != nil {
			if err := r.toolHistory.NativeCommitted(records[0].Sequence, nil); err != nil {
				r.poisoned.Store(true)
				return err
			}
		}
		check.state.DeliverySequence, check.state.DetailSequence = records[0].Sequence, records[0].Sequence
		if err := r.publishStop(plan.TurnID(), check.state, protocol.StopHookDelivered); err != nil {
			return err
		}
	}
	failed, err := session.NewTurnFailedDraft(plan.TurnID(), session.TurnFailedPayload{Code: "session_interrupted", Message: "session turn was interrupted"})
	if err != nil {
		return err
	}
	_, err = r.commitHookFacts(ctx, []session.RecordDraft{failed})
	return err
}
