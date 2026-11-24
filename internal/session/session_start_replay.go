package session

import (
	"slices"

	"easycode/internal/domain"
)

// sessionStartReplay 校验一次检查的冻结前缀和真实交付位置。
type sessionStartReplay struct {
	plan     SessionStartPlan
	started  *domain.HookRunIdentity
	finished []SessionStartFinishedPayload
}

func (r *sessionStartReplay) acceptsNext(identity domain.HookRunIdentity) bool {
	target, _ := identity.Target.SessionStart()
	index := len(r.finished)
	if r.started != nil || target != r.plan.target || identity.PlanDigest != r.plan.digest ||
		identity.Ordinal != index || index >= len(r.plan.checks) {
		return false
	}
	return r.lastStatus() != domain.HookCancel && r.lastStatus() != domain.HookUncertain
}

func (r *sessionStartReplay) lastStatus() domain.HookOutcome {
	if len(r.finished) == 0 {
		return ""
	}
	return r.finished[len(r.finished)-1].Result.Status
}

func (r *sessionStartReplay) selectedProofs() []domain.SessionStartProof {
	var proofs []domain.SessionStartProof
	remaining := domain.MaxSessionStartContextBytes
	for _, result := range r.finished {
		proof := result.Proof
		if proof == nil {
			continue
		}
		if proof.Bytes > remaining {
			break
		}
		proofs = append(proofs, *proof)
		remaining -= proof.Bytes
	}
	return proofs
}

func (r *sessionStartReplay) acceptsClosure(close SessionStartClosedPayload) bool {
	count := len(r.finished)
	if close.Target != r.plan.target || close.PlanDigest != r.plan.digest || close.Finished != count {
		return false
	}
	status := r.lastStatus()
	if close.Origin == "recovery" {
		uncertain := r.started != nil || status == domain.HookUncertain
		return (uncertain && close.State == SessionStartUncertain) || (!uncertain && close.State == SessionStartInterrupted)
	}
	if r.started != nil || (status == domain.HookUncertain) != (close.State == SessionStartUncertain) ||
		(status == domain.HookCancel && close.State != SessionStartCancelled) {
		return false
	}
	if close.State == SessionStartUncertain || close.State == SessionStartCancelled {
		return true
	}
	proofs := r.selectedProofs()
	hasCandidates := slices.ContainsFunc(r.finished, func(result SessionStartFinishedPayload) bool {
		return result.Proof != nil
	})
	allFinished := count == len(r.plan.checks)
	switch close.State {
	case SessionStartApplied:
		return slices.Equal(proofs, close.Proofs) &&
			(allFinished && close.Code == domain.HookOK || !allFinished && close.Code == domain.HookExecutionBudgetExhausted)
	case SessionStartNoContext:
		return allFinished && !hasCandidates && !slices.ContainsFunc(r.finished, func(result SessionStartFinishedPayload) bool {
			return result.Result.Status != domain.HookContinue
		})
	case SessionStartSkipped:
		switch close.Code {
		case domain.HookExecutionBudgetExhausted:
			return !allFinished && len(proofs) == 0
		case domain.HookFeedbackBudgetExceeded:
			if hasCandidates {
				return true
			}
		case domain.HookTrustDenied, domain.HookPlanLimit, domain.HookConfigInvalid:
			return count == 0
		case domain.HookUnsupported, domain.HookSandboxUnavailable, domain.HookInputLimit:
			if count == 0 {
				return true
			}
		}
		return len(proofs) == 0 && allFinished && slices.ContainsFunc(r.finished, func(result SessionStartFinishedPayload) bool {
			return result.Result.Code == close.Code
		})
	}
	return false
}

func (r *sessionStartReplay) accept(plan *replayAccumulator, active *activeReplayTurn, batch []Record, seenInputs map[domain.InputID]struct{}) (bool, error) {
	first := batch[0]
	switch first.EventKind {
	case EventSessionStartHookStarted:
		identity, err := DecodeSessionStartHookStarted(first)
		if err != nil || len(batch) != 1 || !r.acceptsNext(identity) {
			return false, replayCorrupt("session start execution does not match plan")
		}
		r.started = &identity
	case EventSessionStartHookFinished:
		result, err := DecodeSessionStartHookFinished(first)
		if err != nil || len(batch) != 1 || r.started == nil || result.Result.Identity != *r.started {
			return false, replayCorrupt("session start result has no matching admission")
		}
		if p := result.Proof; p != nil && p.Reference != r.plan.checks[len(r.finished)].Reference {
			return false, replayCorrupt("session start candidate has a different declaration")
		}
		r.started = nil
		r.finished = append(r.finished, result)
	case EventSessionStartHookClosed:
		close, err := DecodeSessionStartHookClosed(first)
		if err != nil || !r.acceptsClosure(close) {
			return false, replayCorrupt("session start closure does not match finished prefix")
		}
		if err := acceptSessionStartDelivery(plan, active, batch, seenInputs, close); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, replayCorrupt("record overlaps an open session start plan")
	}
	return false, nil
}

func acceptSessionStartDelivery(plan *replayAccumulator, active *activeReplayTurn, batch []Record, seenInputs map[domain.InputID]struct{}, close SessionStartClosedPayload) error {
	if close.State != SessionStartApplied {
		if len(batch) != 1 {
			return replayCorrupt("undelivered session start closure is not standalone")
		}
		return nil
	}
	if close.Native.Provider != plan.SessionMetadata.Provider || close.Native.Wire != plan.SessionMetadata.ProviderWire {
		return replayCorrupt("session start native commit provider mismatch")
	}
	compact := close.Target.Source == domain.SessionStartCompact
	if compact && len(batch) != 1 || !compact && (len(batch) != 3 || validateSessionStartBatchPrefix(batch[0], batch[1]) != nil || validateSessionStartBatchPrefix(batch[0], batch[2]) != nil) {
		return replayCorrupt("invalid session start delivery batch")
	}
	plan.recordNative(NativeCommitRecord{
		SessionStart: &SessionStartRestoreEvidence{Target: close.Target, Proofs: slices.Clone(close.Proofs)},
		Role:         domain.NativeCommitSessionStartContext, Sequence: batch[0].Sequence, TurnID: close.Target.TurnID, Commit: *close.Native,
	})
	if !compact {
		return acceptInputBatch(plan, active, batch[1:], seenInputs)
	}
	return nil
}

// SessionStartRecoveryPlan 只携带已验证的未关闭前缀，用于零外部执行的本地补偿。
type SessionStartRecoveryPlan struct{ prefix sessionStartReplay }

// CloseDraft 根据真实执行证据保存中断或结果未知。
func (p SessionStartRecoveryPlan) CloseDraft() (RecordDraft, error) {
	state, code := SessionStartInterrupted, domain.HookInterrupted
	if p.prefix.started != nil || p.prefix.lastStatus() == domain.HookUncertain {
		state, code = SessionStartUncertain, domain.HookOutcomeUncertain
	}
	return sessionStartCloseDraft(SessionStartClosedPayload{
		Target: p.prefix.plan.target, PlanDigest: p.prefix.plan.digest, Finished: len(p.prefix.finished), State: state, Code: code, Origin: "recovery",
	})
}

type sessionStartLedger struct {
	open       *sessionStartReplay
	seenInputs bool
	activation bool
}

func (ledger *sessionStartLedger) begin(record Record, active *activeReplayTurn, compactTarget domain.SessionStartTarget) error {
	plan, err := DecodeSessionStartHookPlanned(record)
	if err != nil || record.BatchSize != 1 {
		return replayCorrupt("invalid session start plan placement")
	}
	target := plan.target
	if target.Source == domain.SessionStartCompact {
		if target != compactTarget {
			return replayCorrupt("session start compact plan does not match the installed checkpoint")
		}
	} else {
		if active == nil || target.TurnID != active.turnID || active.sessionStartChecked || active.initialInputs || active.nextSampleIndex != 0 {
			return replayCorrupt("invalid session start activation placement")
		}
		if target.Source == domain.SessionStartStartup && (ledger.activation || ledger.seenInputs) {
			return replayCorrupt("session start startup follows session activation")
		}
		ledger.activation = true
		active.sessionStartChecked = true
	}
	ledger.open = &sessionStartReplay{plan: plan}
	return nil
}
