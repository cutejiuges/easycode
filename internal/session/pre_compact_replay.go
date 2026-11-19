package session

import (
	"easycode/internal/domain"
	"easycode/internal/protocol"
)

// preCompactReplay 仅持有当前压缩的有界检查前缀。通过后无需恢复命令或重点。
type preCompactReplay struct {
	pending  *domain.HookRunIdentity
	finished []PreCompactHookFinishedPayload
	decision *PreCompactHookDecidedPayload
}

func (state *preCompactReplay) accept(start CompactionStartedPayload, record Record) error {
	if state.decision != nil {
		return replayCorrupt("pre-compact check follows its decision")
	}
	plan := start.PreCompact
	target, err := domain.NewPreCompactHookTarget(start.ID, start.Scope)
	if err != nil {
		return replayCorrupt("pre-compact target is invalid")
	}
	ordinal := len(state.finished)
	switch record.EventKind {
	case EventPreCompactHookStarted:
		id, err := DecodePreCompactHookStartedPayload(record)
		if err != nil || state.pending != nil || ordinal >= len(plan.checks) || !state.continues() || id.Target != target || id.PlanDigest != plan.digest || id.Ordinal != ordinal {
			return replayCorrupt("pre-compact execution does not match the next plan declaration")
		}
		state.pending = &id
	case EventPreCompactHookFinished:
		finished, err := DecodePreCompactHookFinishedPayload(record)
		if err != nil || state.pending == nil || finished.Result.Identity != *state.pending || ordinal >= len(plan.checks) {
			return replayCorrupt("pre-compact result has no matching execution")
		}
		policy := plan.checks[ordinal].Reference.Feedback
		if (finished.Focus != nil && policy != domain.HookFeedbackModel) || (finished.Result.Code == domain.HookFeedbackNotAllowed && policy != domain.HookFeedbackNone) {
			return replayCorrupt("pre-compact result discloses an unauthorized focus")
		}
		state.finished = append(state.finished, finished)
		state.pending = nil
	case EventPreCompactHookDecided:
		decision, err := DecodePreCompactHookDecidedPayload(record)
		if err != nil || state.pending != nil || decision.Target != target || decision.PlanDigest != plan.digest || decision.Finished != ordinal || !state.acceptsDecision(plan, decision) {
			return replayCorrupt("pre-compact decision does not match execution evidence")
		}
		state.decision = &decision
	default:
		return replayCorrupt("invalid pre-compact record")
	}
	return nil
}

func (state *preCompactReplay) continues() bool {
	last := len(state.finished) - 1
	return last < 0 || state.finished[last].Result.Status == domain.HookContinue
}

func (state *preCompactReplay) focusBytes(original int) (int, bool) {
	total := original
	appended := false
	for _, row := range state.finished {
		if row.Focus == nil {
			continue
		}
		if total != 0 {
			total += 2
		}
		total += row.Focus.Bytes
		appended = true
	}
	return total, appended
}

func (state *preCompactReplay) acceptsDecision(plan PreCompactPlan, decision PreCompactHookDecidedPayload) bool {
	count := len(state.finished)
	complete := count == len(plan.checks)
	var last domain.HookRunResult
	if count != 0 {
		last = state.finished[count-1].Result
	}
	bytes, appended := state.focusBytes(plan.original.Bytes)
	continues := state.continues()
	switch decision.Code {
	case "":
		return complete && continues && bytes <= domain.MaxCompactionInstructionsBytes
	case protocol.CompactionFailureHookTrustRequired, protocol.CompactionFailureHookTrustDenied:
		return count == 0
	case protocol.CompactionFailureHookDenied:
		return last.Status == domain.HookBlock && last.Code == decision.Cause
	case protocol.CompactionFailureHookFailed:
		switch decision.Cause {
		case domain.HookExecutionBudgetExhausted:
			return continues && !complete
		case domain.HookUnsupported, domain.HookSandboxUnavailable, domain.HookInputLimit:
			if count == 0 {
				return true
			}
		}
		return last.Status == domain.HookError && last.Code == decision.Cause
	case protocol.CompactionFailureBudget:
		return continues && appended && (complete || bytes > domain.MaxCompactionInstructionsBytes)
	case protocol.CompactionFailureUnknownBudget:
		return continues && appended && complete && bytes <= domain.MaxCompactionInstructionsBytes
	case protocol.CompactionFailureCancelled:
		return continues || last.Status == domain.HookCancel
	case protocol.CompactionFailureSession:
		return last.Status == domain.HookUncertain && last.Code == decision.Cause
	default:
		return false
	}
}

func (state *preCompactReplay) allowsSummary(plan PreCompactPlan) bool {
	return plan.kind == preCompactNone || (plan.kind == preCompactPlanned && state.decision != nil && state.decision.Code == "")
}

func (state *preCompactReplay) acceptsFailure(plan PreCompactPlan, failed CompactionFailedPayload) bool {
	if failed.Code == protocol.CompactionFailureInterrupted {
		return failed.UsageState == "unavailable"
	}
	if state.allowsSummary(plan) {
		return !failed.Code.IsHookFailure()
	}
	if failed.UsageState != "unavailable" {
		return false
	}
	switch plan.kind {
	case preCompactInvalid:
		return failed.Code == protocol.CompactionFailureHookConfigInvalid
	case preCompactPlanned:
		return state.decision != nil && failed.Code == state.decision.Code
	default:
		return false
	}
}
