package session

import (
	"slices"

	"easycode/internal/domain"
)

// promptSubmitReplay 持有一个采样安全点的有序运行前缀与输入交付资格。
type promptSubmitReplay struct {
	plan        PromptSubmitPlan
	input       int
	ordinal     int
	started     domain.HookRunIdentity
	last        domain.HookRunResult
	batchReject domain.HookCode
	passed      []domain.InputID
	closed      PromptSubmitCloseState
}

func (plan *replayAccumulator) beginPromptSubmit(active *activeReplayTurn, record Record, seenInputs map[domain.InputID]struct{}) error {
	check, err := DecodePromptSubmitPlanned(record)
	if err != nil || active == nil || record.TurnID != active.turnID ||
		active.promptSubmit != nil || check.fields.TargetSampleIndex != active.nextSampleIndex ||
		active.inputAtSample || active.autoCompactedSample || active.pendingCalls != nil {
		return replayCorrupt("prompt submit plan is outside its input safe point")
	}
	if check.fields.Placement == domain.InputInitial {
		if active.initialInputs || active.sessionStartChecked {
			return replayCorrupt("prompt submit plan followed initial input preparation")
		}
	} else if !active.initialInputs || (active.stop != nil && !active.stop.PendingFeedback()) ||
		(active.stop == nil && (!active.nextSampleReady || !active.toolOutputsCommitted)) {
		return replayCorrupt("prompt submit plan has no continuation safe point")
	}
	if plan.promptSubmitInputs == nil {
		plan.promptSubmitInputs = make(map[domain.InputID]struct{})
	}
	for _, id := range check.fields.InputIDs {
		_, checked := plan.promptSubmitInputs[id]
		_, committed := seenInputs[id]
		if checked || committed {
			return replayCorrupt("prompt submit input identity is duplicated")
		}
		plan.promptSubmitInputs[id] = struct{}{}
	}
	active.promptSubmit = &promptSubmitReplay{plan: check}
	active.lastSequence = record.Sequence
	return nil
}

func (check *promptSubmitReplay) open() bool {
	return check != nil && check.closed == ""
}

// allowsFollowing 仅允许中断检查的 turn 收口及已应允的 Stop 单独反馈。
func (check *promptSubmitReplay) allowsFollowing(active *activeReplayTurn, batch []Record) bool {
	if check == nil {
		return true
	}
	kind := batch[0].EventKind
	if check.closed != PromptSubmitCompleted {
		return kind == EventTurnFailed || (active.stop != nil && active.stop.PendingFeedback() &&
			kind == EventProviderNativeCommit && len(batch) == 1)
	}
	if check.plan.fields.Placement == domain.InputInitial && len(check.passed) == 0 {
		return kind == EventTurnFailed
	}
	return true
}

func (check *promptSubmitReplay) accept(batch []Record) error {
	if len(batch) != 1 || batch[0].TurnID != check.plan.fields.TurnID {
		return replayCorrupt("prompt submit record scope does not match the open plan")
	}
	record := batch[0]
	switch record.EventKind {
	case EventHookExecutionStarted:
		return check.acceptStarted(record)
	case EventHookExecutionFinished:
		return check.acceptFinished(record)
	case EventPromptSubmitHookDecided:
		return check.acceptDecision(record)
	case EventPromptSubmitHookClosed:
		return check.acceptClosure(record)
	default:
		return replayCorrupt("record overlaps an open prompt submit plan")
	}
}

func (check *promptSubmitReplay) acceptStarted(record Record) error {
	if check.input >= len(check.plan.fields.InputIDs) || check.ordinal >= len(check.plan.fields.Checks) ||
		check.started.Event != "" || check.batchReject != "" ||
		(check.last.Status != "" && check.last.Status != domain.HookContinue) {
		return replayCorrupt("prompt submit execution crossed a completed prefix")
	}
	id, err := DecodeHookExecutionStarted(record)
	target, targetErr := domain.NewUserPromptSubmitHookTarget(check.plan.Target(check.plan.fields.InputIDs[check.input]))
	expected, identityErr := domain.NewUserPromptSubmitHookRunIdentity(target, check.plan.digest, check.ordinal)
	if err != nil || targetErr != nil || identityErr != nil || id != expected {
		return replayCorrupt("prompt submit execution does not match plan")
	}
	check.started = id
	return nil
}

func (check *promptSubmitReplay) acceptFinished(record Record) error {
	finished, err := DecodeHookExecutionFinished(record)
	if err != nil || check.started.Event == "" ||
		finished.Result.Identity != check.started || finished.Feedback.Reference != check.plan.fields.Checks[check.ordinal] {
		return replayCorrupt("prompt submit result has no matching admission")
	}
	check.started = domain.HookRunIdentity{}
	check.ordinal++
	check.last = finished.Result
	return nil
}

func (check *promptSubmitReplay) acceptDecision(record Record) error {
	value, err := DecodePromptSubmitDecided(record)
	if err != nil || value.PlanDigest != check.plan.digest || check.input >= len(check.plan.fields.InputIDs) ||
		value.Target != check.plan.Target(check.plan.fields.InputIDs[check.input]) ||
		check.started.Event != "" || !check.validDecision(value.Code) {
		return replayCorrupt("prompt submit decision does not match its execution prefix")
	}
	if value.Code == domain.HookOK {
		check.passed = append(check.passed, value.Target.InputID)
	}
	check.input++
	check.ordinal = 0
	check.last = domain.HookRunResult{}
	return nil
}

func (check *promptSubmitReplay) validDecision(code domain.HookCode) bool {
	if check.last.Status != "" && check.last.Status != domain.HookContinue {
		return (check.last.Status == domain.HookError || check.last.Status == domain.HookBlock) && code == check.last.Code
	}
	if check.ordinal == len(check.plan.fields.Checks) {
		return code == domain.HookOK
	}
	if check.batchReject != "" {
		return code == check.batchReject
	}
	switch code {
	case domain.HookTrustRequired, domain.HookUnsupported, domain.HookSandboxUnavailable:
		if check.input != 0 || check.ordinal != 0 {
			return false
		}
		check.batchReject = code
		return true
	case domain.HookExecutionBudgetExhausted:
		check.batchReject = code
		return true
	case domain.HookTrustDenied:
		return true
	case domain.HookInputLimit:
		return check.ordinal == 0
	default:
		return false
	}
}

func (check *promptSubmitReplay) acceptClosure(record Record) error {
	value, err := decodePromptSubmitClosed(record)
	if err != nil || value.PlanDigest != check.plan.digest || check.started.Event != "" {
		return replayCorrupt("prompt submit closure does not match its execution prefix")
	}
	uncertain := check.last.Status == domain.HookUncertain
	switch value.State {
	case PromptSubmitCompleted:
		if check.input != len(check.plan.fields.InputIDs) {
			return replayCorrupt("completed prompt submit plan lacks input decisions")
		}
	case PromptSubmitCancelled:
		if uncertain {
			return replayCorrupt("cancelled prompt submit plan has an uncertain result")
		}
	case PromptSubmitUncertain:
		if !uncertain || check.last.Origin != "execution" {
			return replayCorrupt("uncertain prompt submit plan has no matching execution")
		}
	}
	check.closed = value.State
	return nil
}

// acceptPromptSubmitInputs 校验三个原生交付入口共用的输入关联。
func (plan *replayAccumulator) acceptPromptSubmitInputs(active *activeReplayTurn, ids []domain.InputID) error {
	if active != nil && active.promptSubmit != nil {
		check := active.promptSubmit
		if check.closed != PromptSubmitCompleted || !slices.Equal(check.passed, ids) {
			return replayCorrupt("native input association does not match completed prompt decisions")
		}
		return nil
	}
	for _, id := range ids {
		if _, checked := plan.promptSubmitInputs[id]; checked {
			return replayCorrupt("prompt submit input identity was reused outside its plan")
		}
	}
	return nil
}

// tornInputAssociations 只保留可见的关联行及其批次首种类，不保留原生内容。
type tornInputAssociations struct {
	firstKind EventKind
	records   []Record
}

func collectTornInputAssociations(batch []Record) tornInputAssociations {
	var result tornInputAssociations
	for _, record := range batch {
		if record.EventKind == EventTurnInputsCommitted {
			result.firstKind = batch[0].EventKind
			result.records = append(result.records, record)
		}
	}
	return result
}

func (plan *replayAccumulator) validateTornPromptInputAssociations(active *activeReplayTurn, tail tornInputAssociations, seen map[domain.InputID]struct{}, sessionStartOpen, compactionOpen bool) error {
	if len(plan.promptSubmitInputs) == 0 {
		return nil
	}
	for _, record := range tail.records {
		payload, err := DecodeTurnInputsPayload(record)
		if err != nil {
			return replayCorrupt("torn prompt submit input association is invalid")
		}
		if active == nil || active.promptSubmit == nil {
			if err := plan.acceptPromptSubmitInputs(active, payload.InputIDs); err != nil {
				return err
			}
			continue
		}
		if len(tail.records) != 1 || record.TurnID != active.turnID || active.pendingCalls != nil || compactionOpen {
			return replayCorrupt("torn prompt submit input association crossed its safe point")
		}
		stopDelivery := active.stop != nil && active.stop.PendingFeedback()
		if !tail.matchesShape(record, stopDelivery, sessionStartOpen) {
			return replayCorrupt("torn prompt submit input association has invalid batch shape")
		}
		if err := validateBoundInputAssociation(plan, active, payload, seen, stopDelivery); err != nil {
			return err
		}
	}
	return nil
}

func (tail tornInputAssociations) matchesShape(record Record, stopDelivery, sessionStartOpen bool) bool {
	switch {
	case stopDelivery:
		return !sessionStartOpen && tail.firstKind == EventProviderNativeCommit && record.BatchSize == 3 && record.BatchIndex == 1
	case sessionStartOpen:
		return tail.firstKind == EventSessionStartHookClosed && record.BatchSize == 3 && record.BatchIndex == 1
	default:
		return tail.firstKind == EventTurnInputsCommitted && record.BatchSize == 2 && record.BatchIndex == 0
	}
}

// PromptSubmitRecoveryPlan 只持有合法但尚未关闭的检查前缀。
type PromptSubmitRecoveryPlan struct{ prefix promptSubmitReplay }

// CloseDrafts 逐个提供待 Sync 的单记录，允许恢复再次中断。
func (recovery PromptSubmitRecoveryPlan) CloseDrafts() ([]RecordDraft, error) {
	check := recovery.prefix
	var drafts []RecordDraft
	if check.started.Event != "" {
		finished, err := domain.NewEmptyHookFinished(domain.HookRunResult{
			Identity: check.started, Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain, Origin: "recovery",
		}, check.plan.fields.Checks[check.ordinal])
		if err != nil {
			return nil, err
		}
		draft, err := NewHookExecutionFinishedDraft(check.plan.fields.TurnID, finished)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, draft)
	}
	closed, err := NewPromptSubmitClosedDraft(check.plan, PromptSubmitInterrupted)
	if err != nil {
		return nil, err
	}
	return append(drafts, closed), nil
}
