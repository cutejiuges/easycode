package runtime

import (
	"context"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

// appendPreFacts 在成功 Sync 后安装同一批次的完整快照，绝不发布未提交的候选。
func (r *Runtime) appendPreFacts(ctx context.Context, turn domain.TurnID, state *toolExecutionCall, drafts []session.RecordDraft) error {
	records, err := r.commitHookFacts(ctx, drafts)
	if err != nil {
		return err
	}
	if len(records) == 0 || len(records) != len(drafts) {
		r.poisoned.Store(true)
		return fault.New(fault.CodeSessionWrite, "pre hook commit has no durable sequence")
	}
	var snapshot session.ReplayedHookGate
	if state.pre != nil {
		snapshot = state.pre.Clone()
	}
	previous := state.detailSequence
	for i, committed := range records {
		if committed.Sequence == 0 || committed.Sequence <= previous {
			r.poisoned.Store(true)
			return fault.New(fault.CodeSessionWrite, "pre hook commit sequence is invalid")
		}
		previous = committed.Sequence
		// 字段来自本批冻结 draft，持久化位置只采用 writer 返回的真实 sequence。
		record := session.Record{Sequence: committed.Sequence, TurnID: turn, EventKind: drafts[i].EventKind(), Payload: drafts[i].PayloadBytes()}
		switch record.EventKind {
		case session.EventToolHookGatePlanned:
			p, decodeErr := session.DecodeToolHookGatePlanned(record)
			if decodeErr != nil || snapshot.PlanSequence != 0 {
				err = domain.HookConfigInvalid
				break
			}
			snapshot.Plan = p.Clone()
			snapshot.PlanSequence = record.Sequence
		case session.EventHookExecutionStarted:
			id, decodeErr := session.DecodeHookExecutionStarted(record)
			if decodeErr != nil || id.Event != domain.HookPreToolUse || snapshot.Started != nil {
				err = domain.HookConfigInvalid
				break
			}
			snapshot.Started = &id
			snapshot.StartedSequence = record.Sequence
		case session.EventHookExecutionFinished:
			f, decodeErr := session.DecodeHookExecutionFinished(record)
			if decodeErr != nil || snapshot.Started == nil || f.Result.Identity != *snapshot.Started {
				err = domain.HookConfigInvalid
				break
			}
			snapshot.Finished = append(snapshot.Finished, f.Clone())
			snapshot.Started = nil
			snapshot.StartedSequence = 0
		case session.EventToolHookGateDecided:
			d, decodeErr := session.DecodeToolHookGateDecided(record)
			if decodeErr != nil || snapshot.Decision != nil {
				err = domain.HookConfigInvalid
				break
			}
			snapshot.Decision = &d
			snapshot.DecisionSequence = record.Sequence
		case session.EventToolCallResult:
			// 拒绝结果与 gate 同批，投影的选择 sequence 仍来自 gate。
		default:
			err = domain.HookConfigInvalid
		}
		if err != nil {
			r.poisoned.Store(true)
			return fault.New(fault.CodeSessionWrite, "invalid committed pre hook snapshot")
		}
	}
	if err := snapshot.Validate(state.original); err != nil {
		r.poisoned.Store(true)
		return fault.New(fault.CodeSessionWrite, "invalid committed pre hook input chain")
	}
	sequence := records[len(records)-1].Sequence
	if snapshot.Decision != nil {
		sequence = snapshot.DecisionSequence
	}
	cloned := snapshot.Clone()
	state.pre = &cloned
	state.detailSequence = sequence
	if r.hookHistory != nil {
		if err := r.hookHistory.PreHooksCommitted(state.original.Clone(), sequence, snapshot); err != nil {
			r.poisoned.Store(true)
			return fault.Wrap(fault.CodeSessionWrite, "install pre hook snapshot failed", err)
		}
	}
	activity, err := tool.ProjectPreActivity(state.original, snapshot.Plan, snapshot.Finished, snapshot.Started, snapshot.Decision, sequence)
	if err != nil {
		r.poisoned.Store(true)
		return err
	}
	if _, err := protocol.NewHookActivity(activity); err != nil {
		r.poisoned.Store(true)
		return err
	}
	r.emitHookActivity(turn, activity)
	return nil
}

func (state *toolExecutionCall) checkingInput() (domain.HookToolInput, error) {
	if state.pre == nil {
		return tool.HookInputForInvocation(state.original)
	}
	return tool.CheckingHookInput(state.original, state.pre.Plan, state.pre.Finished)
}
