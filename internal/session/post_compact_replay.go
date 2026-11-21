package session

import (
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

// postCompactReplay 跟踪已成功安装 checkpoint 之后的独立检查前缀。
type postCompactReplay struct {
	target   domain.HookTarget
	plan     PostCompactPlan
	pending  *domain.HookRunIdentity
	finished []domain.HookRunResult
}

func (state *postCompactReplay) acceptsNext(id domain.HookRunIdentity) bool {
	if state.pending != nil || state.plan.kind != PostCompactPlanPlanned || len(state.finished) >= state.plan.Count() ||
		id.Event != domain.HookPostCompact || id.Target != state.target || id.PlanDigest != state.plan.digest || id.Ordinal != len(state.finished) {
		return false
	}
	if count := len(state.finished); count > 0 {
		last := state.finished[count-1].Status
		return last != domain.HookCancel && last != domain.HookUncertain
	}
	return true
}

func (state *postCompactReplay) closed(origin postCompactCloseOrigin, cause domain.HookCode) (PostCompactHookClosedPayload, error) {
	if state.plan.kind != PostCompactPlanPlanned || len(state.finished) > state.plan.Count() ||
		(state.pending != nil && origin != postCompactRecovery) {
		return PostCompactHookClosedPayload{}, fmt.Errorf("post-compact prefix cannot be closed")
	}
	status, code := state.verdict(origin, cause)
	payload := PostCompactHookClosedPayload{
		Target: state.target, PlanDigest: state.plan.digest, Finished: len(state.finished), State: status, Code: code, Origin: origin,
	}
	return payload, payload.Validate()
}

func (state *postCompactReplay) verdict(origin postCompactCloseOrigin, cause domain.HookCode) (protocol.PostCompactStatus, domain.HookCode) {
	count := len(state.finished)
	if state.pending != nil || (count > 0 && state.finished[count-1].Status == domain.HookUncertain) {
		return protocol.PostCompactUncertain, domain.HookOutcomeUncertain
	}
	if count > 0 && state.finished[count-1].Status == domain.HookCancel {
		return protocol.PostCompactCancelled, domain.HookCancelled
	}
	if count == state.plan.Count() {
		for _, result := range state.finished {
			if result.Status == domain.HookError {
				return protocol.PostCompactFailed, result.Code
			}
		}
		return protocol.PostCompactSucceeded, domain.HookOK
	}
	if origin == postCompactRecovery {
		return protocol.PostCompactInterrupted, domain.HookInterrupted
	}
	switch {
	case cause == domain.HookCancelled:
		return protocol.PostCompactCancelled, cause
	case cause == domain.HookExecutionBudgetExhausted && count > 0:
		return protocol.PostCompactFailed, cause
	case count == 0 && protocol.PostCompactSkipped.ValidCode(cause):
		return protocol.PostCompactSkipped, cause
	default:
		return "", ""
	}
}

func (state *postCompactReplay) accept(record Record) error {
	switch record.EventKind {
	case EventPostCompactHookStarted:
		id, err := DecodePostCompactHookStartedPayload(record)
		if err != nil || !state.acceptsNext(id) {
			return replayCorrupt("post-compact execution does not match the next planned declaration")
		}
		state.pending = &id
	case EventPostCompactHookFinished:
		result, err := DecodePostCompactHookFinishedPayload(record)
		if err != nil || state.pending == nil || result.Identity != *state.pending {
			return replayCorrupt("post-compact result has no matching admission")
		}
		state.pending = nil
		state.finished = append(state.finished, result)
	case EventPostCompactHookClosed:
		got, err := DecodePostCompactHookClosedPayload(record)
		if err != nil {
			return replayCorrupt("post-compact closure is invalid")
		}
		want, err := state.closed(got.Origin, got.Code)
		if err != nil || want != got {
			return replayCorrupt("post-compact closure does not match execution evidence")
		}
	default:
		return replayCorrupt("record overlaps post-compact observation")
	}
	return nil
}

// PostCompactRecoveryPlan 仅包含已通过完整回放的成功检查点与未关闭前缀。
type PostCompactRecoveryPlan struct{ state postCompactReplay }

// CloseDraft 从已有事实本地结算前缀，保留已准入但无结果的序位。
func (plan PostCompactRecoveryPlan) CloseDraft() (RecordDraft, error) {
	payload, err := plan.state.closed(postCompactRecovery, "")
	if err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventPostCompactHookClosed, "", "", payload)
}
