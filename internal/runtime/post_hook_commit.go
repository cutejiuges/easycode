package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

// appendPostFacts 在 Sync 后安装宿主快照，再对该批次发布唯一刷新事件。
func (r *Runtime) appendPostFacts(ctx context.Context, turn domain.TurnID, state *postHookCall, phase protocol.PostHookPhase, drafts []session.RecordDraft) error {
	records, err := r.commitHookFacts(ctx, drafts)
	if err != nil {
		return err
	}
	if len(records) != len(drafts) || len(records) == 0 || records[len(records)-1].Sequence == 0 || records[len(records)-1].Sequence <= state.detailSequence {
		r.poisoned.Store(true)
		return fault.New(fault.CodeSessionWrite, "post hook commit has no durable sequence")
	}
	sequence := records[len(records)-1].Sequence
	state.detailSequence = sequence
	snapshot := state.history.Clone()
	switch phase {
	case protocol.PostHookPlanned:
		snapshot.Plan = state.plan.Clone()
		snapshot.PlanSequence = sequence
	case protocol.PostHookStarted:
		id, _ := domain.NewPostHookRunIdentity(state.plan, len(state.finished))
		snapshot.Started = &id
		snapshot.StartedSequence = sequence
	case protocol.PostHookFinished:
		snapshot.Finished = append([]domain.HookFinished(nil), state.finished...)
		snapshot.Started = nil
		snapshot.StartedSequence = 0
	case protocol.PostHookCompleted:
		snapshot.Finished = append([]domain.HookFinished(nil), state.finished...)
		snapshot.Started = nil
		snapshot.StartedSequence = 0
		terminal, decodeErr := session.DecodeToolPostHookCompleted(session.Record{EventKind: drafts[len(drafts)-1].EventKind(), Payload: drafts[len(drafts)-1].PayloadBytes()})
		if decodeErr != nil {
			r.poisoned.Store(true)
			return decodeErr
		}
		snapshot.Terminal = &terminal
		snapshot.TerminalSequence = sequence
	}
	state.history = snapshot.Clone()
	if r.hookHistory != nil {
		if err := r.hookHistory.PostHooksCommitted(tool.InvocationID(state.plan.Plan.InvocationID), sequence, snapshot); err != nil {
			r.poisoned.Store(true)
			return err
		}
	}
	if r.activityEmitter != nil {
		event, err := protocol.NewPostHookCommitted(protocol.PostHookCommittedPayload{InvocationID: state.plan.Plan.InvocationID, Event: domain.HookPostToolUse, PlanDigest: state.plan.Digest, DetailSequence: sequence, Phase: phase})
		if err != nil {
			r.poisoned.Store(true)
			return err
		}
		r.activityEmitter(r.decorate(event, turn))
	}
	return nil
}
