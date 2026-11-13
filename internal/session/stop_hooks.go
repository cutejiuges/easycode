package session

import (
	"fmt"

	"easycode/internal/domain"
)

func NewTurnStopHookPlannedDraft(plan domain.StopPlan) (RecordDraft, error) {
	if plan.Validate() != nil {
		return RecordDraft{}, fmt.Errorf("invalid stop hook plan")
	}
	return encodeDraft(EventTurnStopHookPlanned, "", plan.Sample().TurnID, plan.Clone())
}
func NewTurnStopHookDecidedDraft(decision domain.StopDecision) (RecordDraft, error) {
	if decision.Validate() != nil {
		return RecordDraft{}, fmt.Errorf("invalid stop hook decision")
	}
	sample, _ := decision.Target.AssistantSample()
	return encodeDraft(EventTurnStopHookDecided, "", sample.TurnID, decision.Clone())
}
func DecodeTurnStopHookPlanned(record Record) (domain.StopPlan, error) {
	if record.EventKind != EventTurnStopHookPlanned {
		return domain.StopPlan{}, fmt.Errorf("invalid stop plan kind")
	}
	value, err := domain.DecodeStopPlan(record.Payload)
	if err != nil || value.Sample().TurnID != record.TurnID {
		return domain.StopPlan{}, fmt.Errorf("invalid stop plan scope")
	}
	return value, nil
}
func DecodeTurnStopHookDecided(record Record) (domain.StopDecision, error) {
	if record.EventKind != EventTurnStopHookDecided {
		return domain.StopDecision{}, fmt.Errorf("invalid stop decision kind")
	}
	value, err := domain.DecodeStopDecision(record.Payload)
	sample, _ := value.Target.AssistantSample()
	if err != nil || sample.TurnID != record.TurnID {
		return domain.StopDecision{}, fmt.Errorf("invalid stop decision scope")
	}
	return value, nil
}

// ReplayedStop 保存从 durable 边界构成的检查快照，不保存配置或进程权限。
type ReplayedStop struct {
	Plan             domain.StopPlan
	PlanSequence     uint64
	DetailSequence   uint64
	Started          *domain.HookRunIdentity
	StartedSequence  uint64
	Finished         []domain.HookFinished
	Decision         *domain.StopDecision
	DecisionSequence uint64
	DeliverySequence uint64
}

func (s ReplayedStop) Clone() ReplayedStop {
	s.Plan = s.Plan.Clone()
	s.Finished = append([]domain.HookFinished(nil), s.Finished...)
	if s.Started != nil {
		v := *s.Started
		s.Started = &v
	}
	if s.Decision != nil {
		v := s.Decision.Clone()
		s.Decision = &v
	}
	return s
}
func (s ReplayedStop) PendingFeedback() bool {
	return s.Decision != nil && s.Decision.Disposition == domain.StopContinued && s.DeliverySequence == 0
}
func (s ReplayedStop) Validate() error {
	if s.Plan.Validate() != nil || s.PlanSequence < 3 || s.DetailSequence < s.PlanSequence || len(s.Finished) > len(s.Plan.Checks()) {
		return fmt.Errorf("invalid stop history")
	}
	refs := s.Plan.Checks()
	for i, f := range s.Finished {
		id, err := domain.NewStopHookRunIdentity(s.Plan, i)
		if err != nil || f.Validate() != nil || f.Result.Identity != id || f.Feedback.Reference != refs[i] || f.Feedback.Kind != domain.HookFeedbackEmpty || i < len(s.Finished)-1 && f.Result.Status != domain.HookContinue {
			return fmt.Errorf("invalid stop execution prefix")
		}
	}
	if s.Started != nil {
		id, err := domain.NewStopHookRunIdentity(s.Plan, len(s.Finished))
		if err != nil || *s.Started != id || s.StartedSequence <= s.PlanSequence || s.StartedSequence > s.DetailSequence || s.Decision != nil {
			return fmt.Errorf("invalid pending stop execution")
		}
	} else if s.StartedSequence != 0 {
		return fmt.Errorf("unrelated stop started sequence")
	}
	if s.Decision == nil {
		if s.DecisionSequence != 0 || s.DeliverySequence != 0 || len(s.Finished) > 0 && s.Finished[len(s.Finished)-1].Result.Status != domain.HookContinue || s.Plan.Kind() == domain.StopPlanChecks && len(s.Finished) == len(refs) {
			return fmt.Errorf("unfinished stop decision is missing")
		}
	} else {
		if s.Started != nil || s.DecisionSequence <= s.PlanSequence || s.DecisionSequence > s.DetailSequence || s.Decision.ValidateAgainst(s.Plan, s.Finished) != nil {
			return fmt.Errorf("invalid frozen stop decision")
		}
		if s.DeliverySequence != 0 && (s.Decision.Disposition != domain.StopContinued || s.DeliverySequence <= s.DecisionSequence || s.DeliverySequence > s.DetailSequence) {
			return fmt.Errorf("invalid stop delivery sequence")
		}
	}
	return nil
}

// StopRecoveryPlan 是本地收口输入，不包含队列、配置、runner 或网络策略。
type StopRecoveryPlan struct{ state ReplayedStop }

func NewStopRecoveryPlan(state ReplayedStop) (StopRecoveryPlan, error) {
	v := StopRecoveryPlan{state.Clone()}
	return v, v.Validate()
}
func (p StopRecoveryPlan) TurnID() domain.TurnID   { return p.state.Plan.Sample().TurnID }
func (p StopRecoveryPlan) NextSampleIndex() uint32 { return p.state.Plan.Sample().SampleIndex + 1 }
func (p StopRecoveryPlan) State() ReplayedStop     { return p.state.Clone() }
func (p StopRecoveryPlan) Clone() StopRecoveryPlan { return StopRecoveryPlan{p.state.Clone()} }
func (p StopRecoveryPlan) Validate() error {
	if p.state.Validate() != nil || p.state.Decision != nil && p.state.Decision.Disposition != domain.StopContinued {
		return fmt.Errorf("invalid stop recovery state")
	}
	return nil
}

func beginStopCheck(active *activeReplayTurn, record Record) error {
	p, err := DecodeTurnStopHookPlanned(record)
	if err != nil || active.stop != nil || p.Sample().TurnID != active.turnID || p.Sample().SampleIndex != active.nextSampleIndex || p.Continuations() != active.stopContinuations {
		return replayCorrupt("stop plan does not match committed sample")
	}
	active.stop = &ReplayedStop{Plan: p.Clone(), PlanSequence: record.Sequence, DetailSequence: record.Sequence}
	return nil
}

// acceptStopBatch 独立检查完成语法，不借用工具 output、approval 或调用槽。
func acceptStopBatch(plan *ReplayPlan, active *activeReplayTurn, batch []Record, seenInputs map[domain.InputID]struct{}) (bool, ReplayedTurnState, error) {
	s := active.stop
	if s == nil {
		return false, "", nil
	}
	if !batchHasTurn(batch, active.turnID) {
		return true, "", replayCorrupt("stop fact scope mismatch")
	}
	first := batch[0]
	if s.DeliverySequence != 0 {
		if first.EventKind == EventProviderNativeCommit && len(batch) >= 3 && batch[1].EventKind == EventSampleUsage {
			plan.Stops = append(plan.Stops, s.Clone())
			active.stop = nil
			return false, "", nil
		}
		if first.EventKind == EventTurnFailed && len(batch) == 1 {
			if _, err := DecodeTurnFailedPayload(first); err != nil {
				return true, "", err
			}
			plan.Stops = append(plan.Stops, s.Clone())
			return true, ReplayedTurnFailed, nil
		}
		return true, "", replayCorrupt("stop delivery must precede sampling or turn failure")
	}
	if s.PendingFeedback() {
		return true, "", acceptStopDelivery(plan, active, batch, seenInputs)
	}
	switch first.EventKind {
	case EventHookExecutionStarted:
		id, err := DecodeHookExecutionStarted(first)
		expected, idErr := domain.NewStopHookRunIdentity(s.Plan, len(s.Finished))
		if err != nil || idErr != nil || id != expected || len(batch) != 1 || s.Started != nil || s.Decision != nil {
			return true, "", replayCorrupt("invalid stop execution admission")
		}
		if len(s.Finished) > 0 && s.Finished[len(s.Finished)-1].Result.Status != domain.HookContinue {
			return true, "", replayCorrupt("stop execution crossed blocking prefix")
		}
		s.Started = &id
		s.StartedSequence = first.Sequence
		s.DetailSequence = first.Sequence
		active.lastSequence = first.Sequence
		return true, "", s.Validate()
	case EventHookExecutionFinished:
		finished, err := DecodeHookExecutionFinished(first)
		refs := s.Plan.Checks()
		ordinal := len(s.Finished)
		if err != nil || s.Started == nil || ordinal >= len(refs) || finished.Result.Identity != *s.Started || finished.Feedback.Reference != refs[ordinal] || finished.Feedback.Kind != domain.HookFeedbackEmpty {
			return true, "", replayCorrupt("invalid stop execution result")
		}
		s.Finished = append(s.Finished, finished)
		s.Started = nil
		s.StartedSequence = 0
		s.DetailSequence = first.Sequence
		active.lastSequence = first.Sequence
		if len(batch) == 1 {
			if finished.Result.Status != domain.HookContinue || len(s.Finished) >= len(refs) {
				return true, "", replayCorrupt("stop final result lacks atomic decision")
			}
			return true, "", s.Validate()
		}
		state, err := acceptStopDecision(plan, active, batch[1:])
		return true, state, err
	case EventTurnStopHookDecided:
		state, err := acceptStopDecision(plan, active, batch)
		return true, state, err
	default:
		return true, "", replayCorrupt("ordinary facts entered an unfinished stop check")
	}
}

func acceptStopDecision(plan *ReplayPlan, active *activeReplayTurn, batch []Record) (ReplayedTurnState, error) {
	s := active.stop
	if s == nil || s.Started != nil || s.Decision != nil || len(batch) == 0 || batch[0].EventKind != EventTurnStopHookDecided {
		return "", replayCorrupt("invalid stop decision placement")
	}
	decision, err := DecodeTurnStopHookDecided(batch[0])
	if err != nil || decision.ValidateAgainst(s.Plan, s.Finished) != nil {
		return "", replayCorrupt("stop decision differs from frozen prefix")
	}
	if decision.Disposition == domain.StopContinued {
		feedback, _ := decision.Feedback()
		if len(batch) != 1 || active.stopFeedbackBytes+feedback.Bytes > domain.MaxTurnStopFeedbackBytes {
			return "", replayCorrupt("stop continuation budget or batch is invalid")
		}
		active.stopFeedbackBytes += feedback.Bytes
		active.stopContinuations++
	} else {
		if len(batch) != 2 {
			return "", replayCorrupt("stop terminal must be atomic with decision")
		}
		if decision.Disposition == domain.StopPassed {
			if batch[1].EventKind != EventTurnCompleted {
				return "", replayCorrupt("passing stop has no successful terminal")
			}
			if _, err := DecodeTurnCompletedPayload(batch[1]); err != nil {
				return "", err
			}
		} else {
			failed, err := DecodeTurnFailedPayload(batch[1])
			if err != nil || failed.Code != string(decision.Code) {
				return "", replayCorrupt("stop failure differs from decision")
			}
		}
	}
	s.Decision = &decision
	s.DecisionSequence = batch[0].Sequence
	s.DetailSequence = batch[len(batch)-1].Sequence
	active.lastSequence = s.DetailSequence
	if err := s.Validate(); err != nil {
		return "", err
	}
	if decision.Disposition == domain.StopContinued {
		return "", nil
	}
	plan.Stops = append(plan.Stops, s.Clone())
	if decision.Disposition == domain.StopPassed {
		return ReplayedTurnCompleted, nil
	}
	return ReplayedTurnFailed, nil
}

func acceptStopDelivery(plan *ReplayPlan, active *activeReplayTurn, batch []Record, seenInputs map[domain.InputID]struct{}) error {
	s := active.stop
	if s == nil || !s.PendingFeedback() || batch[0].EventKind != EventProviderNativeCommit || len(batch) != 1 && len(batch) != 3 {
		return replayCorrupt("invalid stop delivery batch")
	}
	if len(batch) == 3 && (batch[1].EventKind != EventTurnInputsCommitted || batch[2].EventKind != EventProviderNativeCommit) {
		return replayCorrupt("invalid compound stop delivery roles")
	}
	commit, err := DecodeNativeCommitPayload(batch[0])
	if err != nil {
		return err
	}
	feedback, _ := s.Decision.Feedback()
	plan.NativeCommits = append(plan.NativeCommits, NativeCommitRecord{Sequence: batch[0].Sequence, TurnID: active.turnID, Role: domain.NativeCommitHookFeedback, Feedback: &feedback, Commit: cloneNativeCommit(commit)})
	if len(batch) == 3 {
		if err := acceptBoundInputBatch(plan, active, batch[1:], seenInputs, true); err != nil {
			return err
		}
	}
	s.DeliverySequence = batch[0].Sequence
	s.DetailSequence = batch[len(batch)-1].Sequence
	active.lastSequence = s.DetailSequence
	active.nextSampleReady = true
	return s.Validate()
}
