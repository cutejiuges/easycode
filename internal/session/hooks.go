package session

import (
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

// NewToolHookGatePlannedDraft 冻结当前调用的有序检查引用，不保存可执行命令。
func NewToolHookGatePlannedDraft(turn domain.TurnID, p domain.HookPlan) (RecordDraft, error) {
	if !turn.Valid() || p.Validate() != nil || p.Event != domain.HookPreToolUse {
		return RecordDraft{}, fmt.Errorf("invalid hook plan")
	}
	return encodeDraft(EventToolHookGatePlanned, "", turn, p.Clone())
}

func validHookDeniedResult(call ReplayedToolCall, result tool.InvocationResult) bool {
	if call.Hooks == nil || call.Hooks.Decision == nil {
		return false
	}
	d := call.Hooks.Decision
	if result.Code() != string(d.Code) {
		return false
	}
	switch d.Status {
	case domain.HookBlock:
		return result.Status() == tool.ResultDenied
	case domain.HookError:
		return result.Status() == tool.ResultError
	case domain.HookCancel, domain.HookUncertain:
		return result.Status() == tool.ResultCancelled
	default:
		return false
	}
}
func NewHookExecutionStartedDraft(turn domain.TurnID, id domain.HookRunIdentity) (RecordDraft, error) {
	if !turn.Valid() || id.Validate() != nil {
		return RecordDraft{}, fmt.Errorf("invalid hook execution start")
	}
	if sample, ok := id.Target.AssistantSample(); ok && sample.TurnID != turn {
		return RecordDraft{}, fmt.Errorf("hook execution target mismatch")
	}
	return encodeDraft(EventHookExecutionStarted, "", turn, id)
}
func NewHookExecutionFinishedDraft(turn domain.TurnID, r domain.HookFinished) (RecordDraft, error) {
	if !turn.Valid() || r.Validate() != nil {
		return RecordDraft{}, fmt.Errorf("invalid hook execution result")
	}
	if sample, ok := r.Result.Identity.Target.AssistantSample(); ok && sample.TurnID != turn {
		return RecordDraft{}, fmt.Errorf("hook result target mismatch")
	}
	return encodeDraft(EventHookExecutionFinished, "", turn, r)
}
func NewToolHookGateDecidedDraft(turn domain.TurnID, d domain.HookGateDecision) (RecordDraft, error) {
	if !turn.Valid() || d.Validate() != nil {
		return RecordDraft{}, fmt.Errorf("invalid hook gate decision")
	}
	return encodeDraft(EventToolHookGateDecided, "", turn, d.Clone())
}
func DecodeToolHookGatePlanned(record Record) (domain.HookPlan, error) {
	if record.EventKind != EventToolHookGatePlanned {
		return domain.HookPlan{}, fmt.Errorf("invalid hook plan kind")
	}
	p, err := domain.DecodeHookPlan(record.Payload)
	if err != nil || p.Event != domain.HookPreToolUse {
		return domain.HookPlan{}, fmt.Errorf("invalid pre hook plan event")
	}
	return p, nil
}
func DecodeHookExecutionStarted(record Record) (domain.HookRunIdentity, error) {
	v, err := decodeKnownPayload[domain.HookRunIdentity](record, EventHookExecutionStarted)
	if err != nil {
		return v, err
	}
	if sample, ok := v.Target.AssistantSample(); ok && sample.TurnID != record.TurnID {
		return v, fmt.Errorf("hook start target mismatch")
	}
	return v, v.Validate()
}
func DecodeHookExecutionFinished(record Record) (domain.HookFinished, error) {
	if record.EventKind != EventHookExecutionFinished {
		return domain.HookFinished{}, fmt.Errorf("invalid hook result kind")
	}
	v, err := domain.DecodeHookFinished(record.Payload)
	if sample, ok := v.Result.Identity.Target.AssistantSample(); ok && sample.TurnID != record.TurnID {
		return v, fmt.Errorf("hook result target mismatch")
	}
	return v, err
}
func DecodeToolHookGateDecided(record Record) (domain.HookGateDecision, error) {
	if record.EventKind != EventToolHookGateDecided {
		return domain.HookGateDecision{}, fmt.Errorf("invalid hook decision kind")
	}
	return domain.DecodeHookGateDecision(record.Payload)
}

// ReplayedHookGate 是已校验计划的封闭执行前缀；完成事实不能重新运行。
type ReplayedHookGate struct {
	Plan             domain.HookPlan
	PlanSequence     uint64
	Started          *domain.HookRunIdentity
	StartedSequence  uint64
	Results          []domain.HookRunResult
	Decision         *domain.HookGateDecision
	DecisionSequence uint64
}

func (g ReplayedHookGate) Clone() ReplayedHookGate {
	g.Plan = g.Plan.Clone()
	g.Results = append([]domain.HookRunResult(nil), g.Results...)
	if g.Started != nil {
		v := *g.Started
		g.Started = &v
	}
	if g.Decision != nil {
		v := g.Decision.Clone()
		g.Decision = &v
	}
	return g
}
func (g ReplayedHookGate) Validate(invocation string, cap domain.HookCapability) error {
	if g.Plan.Validate() != nil || g.Plan.Event != domain.HookPreToolUse || g.Plan.InvocationID != invocation || g.Plan.Capability != cap || g.PlanSequence == 0 || len(g.Results) > len(g.Plan.Hooks) {
		return fmt.Errorf("invalid hook recovery plan")
	}
	for i, r := range g.Results {
		id, _ := domain.NewHookRunIdentity(g.Plan, i)
		if r.Validate() != nil || r.Identity != id || i < len(g.Results)-1 && r.Status != domain.HookContinue {
			return fmt.Errorf("invalid hook result prefix")
		}
	}
	if g.Started == nil && g.StartedSequence != 0 || g.Decision == nil && g.DecisionSequence != 0 {
		return fmt.Errorf("orphan hook recovery sequence")
	}
	if g.Started != nil {
		id, err := domain.NewHookRunIdentity(g.Plan, len(g.Results))
		if err != nil || *g.Started != id || g.StartedSequence <= g.PlanSequence {
			return fmt.Errorf("invalid pending hook identity")
		}
	}
	if g.Decision != nil {
		d := g.Decision
		if d.Validate() != nil || g.Started != nil || g.DecisionSequence <= g.PlanSequence || d.InvocationID != invocation || d.PlanDigest != g.Plan.Digest || d.Completed != len(g.Results) {
			return fmt.Errorf("invalid hook recovery decision")
		}
		if err := validateHookDecisionPrefix(g, *d); err != nil {
			return err
		}
	}
	if g.Decision == nil && (len(g.Results) == len(g.Plan.Hooks) || len(g.Results) > 0 && g.Results[len(g.Results)-1].Status != domain.HookContinue) {
		return fmt.Errorf("hook result has no atomic gate decision")
	}
	return nil
}

func (active *activeReplayTurn) hookCall(invocation string) (*ReplayedToolCall, error) {
	for i := range active.pendingCalls {
		call := &active.pendingCalls[i]
		if string(call.Invocation.InvocationID()) != invocation {
			continue
		}
		if !call.Invocation.Capability().Exclusive() || call.State != ReplayedToolCallReady || call.ApprovalSequence != 0 || call.PostHooks != nil {
			return nil, replayCorrupt("hook gate has no pending mutation")
		}
		for j := 0; j < i; j++ {
			if !callComplete(active.pendingCalls[j]) {
				return nil, replayCorrupt("hook gate crossed an unfinished call")
			}
		}
		return call, nil
	}
	return nil, replayCorrupt("hook gate invocation is unknown")
}

func (active *activeReplayTurn) acceptHookBatch(batch []Record) error {
	if !batchHasTurn(batch, active.turnID) {
		return replayCorrupt("hook facts have invalid turn")
	}
	first := batch[0]
	if first.EventKind == EventToolPostHookPlanned || first.EventKind == EventToolPostHookCompleted {
		return active.acceptPostHookBatch(batch)
	}
	if first.EventKind == EventHookExecutionStarted {
		id, err := DecodeHookExecutionStarted(first)
		if err != nil {
			return err
		}
		if id.Event.Post() {
			return active.acceptPostHookBatch(batch)
		}
	}
	if first.EventKind == EventHookExecutionFinished {
		f, err := DecodeHookExecutionFinished(first)
		if err != nil {
			return err
		}
		if f.Result.Identity.Event.Post() {
			return active.acceptPostHookBatch(batch)
		}
	}
	switch first.EventKind {
	case EventToolHookGatePlanned:
		p, err := DecodeToolHookGatePlanned(first)
		if err != nil || len(batch) != 1 {
			return replayCorrupt("invalid hook plan batch")
		}
		call, err := active.hookCall(p.InvocationID)
		if err != nil {
			return err
		}
		if call.Hooks != nil || string(p.Capability) != string(call.Invocation.Capability()) {
			return replayCorrupt("duplicate or mismatched hook plan")
		}
		call.Hooks = &ReplayedHookGate{Plan: p.Clone(), PlanSequence: first.Sequence}
	case EventHookExecutionStarted:
		id, err := DecodeHookExecutionStarted(first)
		if err != nil || len(batch) != 1 {
			return replayCorrupt("invalid hook start batch")
		}
		call, err := active.hookCall(id.InvocationID())
		if err != nil {
			return err
		}
		g := call.Hooks
		if g == nil || g.Decision != nil || g.Started != nil || len(g.Results) > 0 && g.Results[len(g.Results)-1].Status != domain.HookContinue {
			return replayCorrupt("invalid hook start transition")
		}
		expected, err := domain.NewHookRunIdentity(g.Plan, len(g.Results))
		if err != nil || id != expected {
			return replayCorrupt("hook start identity mismatch")
		}
		g.Started = &id
		g.StartedSequence = first.Sequence
	case EventHookExecutionFinished:
		finished, err := DecodeHookExecutionFinished(first)
		r := finished.Result
		if err != nil {
			return replayCorrupt("invalid hook finish payload")
		}
		call, err := active.hookCall(r.Identity.InvocationID())
		if err != nil {
			return err
		}
		g := call.Hooks
		if g == nil || g.Decision != nil || g.Started == nil || *g.Started != r.Identity || finished.Feedback.Reference != g.Plan.Hooks[r.Identity.Ordinal] {
			return replayCorrupt("invalid hook finish transition")
		}
		g.Results = append(g.Results, r)
		g.Started = nil
		g.StartedSequence = 0
		if r.Status == domain.HookContinue && len(g.Results) < len(g.Plan.Hooks) {
			if len(batch) != 1 {
				return replayCorrupt("invalid intermediate hook batch")
			}
		} else {
			if len(batch) < 2 || batch[1].EventKind != EventToolHookGateDecided {
				return replayCorrupt("hook finish requires atomic gate decision")
			}
			if err := active.acceptHookDecision(batch[1:], call); err != nil {
				return err
			}
		}
	case EventToolHookGateDecided:
		d, err := DecodeToolHookGateDecided(first)
		if err != nil {
			return replayCorrupt("invalid hook gate payload")
		}
		call, err := active.hookCall(d.InvocationID)
		if err != nil {
			return err
		}
		if err := active.acceptHookDecision(batch, call); err != nil {
			return err
		}
	default:
		return replayCorrupt("invalid hook batch kind")
	}
	active.lastSequence = batch[len(batch)-1].Sequence
	return nil
}

func (active *activeReplayTurn) acceptHookDecision(batch []Record, call *ReplayedToolCall) error {
	d, err := DecodeToolHookGateDecided(batch[0])
	g := call.Hooks
	if err != nil || g == nil || g.Decision != nil || g.Started != nil || d.InvocationID != g.Plan.InvocationID || d.PlanDigest != g.Plan.Digest || d.Completed != len(g.Results) {
		return replayCorrupt("invalid hook gate decision transition")
	}
	if d.Status == domain.HookContinue {
		if len(batch) != 1 || len(g.Results) != len(g.Plan.Hooks) || g.Results[len(g.Results)-1].Status != domain.HookContinue {
			return replayCorrupt("incomplete hook gate continued")
		}
	} else {
		if len(batch) != 2 || batch[1].EventKind != EventToolCallResult {
			return replayCorrupt("hook denial requires atomic tool result")
		}
		if err := validateHookDecisionPrefix(*g, d); err != nil {
			return replayCorrupt("invalid hook decision prefix")
		}
	}
	g.Decision = &d
	g.DecisionSequence = batch[0].Sequence
	if d.Status != domain.HookContinue {
		return active.acceptResult(batch[1])
	}
	return nil
}

// validateHookDecisionPrefix 供离线构造与顺序 replay 共用相同前缀语义。
func validateHookDecisionPrefix(g ReplayedHookGate, d domain.HookGateDecision) error {
	if d.Status == domain.HookContinue {
		if len(g.Results) != len(g.Plan.Hooks) || g.Results[len(g.Results)-1].Status != domain.HookContinue {
			return fmt.Errorf("incomplete hook gate continued")
		}
		return nil
	}
	if len(g.Results) > 0 && g.Results[len(g.Results)-1].Status != domain.HookContinue {
		last := g.Results[len(g.Results)-1]
		if d.Status != last.Status || d.Code != last.Code || *d.BlockingOrdinal != len(g.Results)-1 {
			return fmt.Errorf("hook decision differs from execution")
		}
		return nil
	}
	if len(g.Results) >= len(g.Plan.Hooks) || *d.BlockingOrdinal != len(g.Results) {
		return fmt.Errorf("hook blocking ordinal differs from plan")
	}
	if d.Status == domain.HookBlock && (d.Code == domain.HookTrustRequired || d.Code == domain.HookTrustDenied) || d.Status == domain.HookCancel && (d.Code == domain.HookCancelled || d.Code == domain.HookInterrupted) || d.Status == domain.HookError && d.Code == domain.HookTimedOut {
		return nil
	}
	return fmt.Errorf("unexecuted hook decision is invalid")
}
