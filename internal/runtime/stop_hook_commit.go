package runtime

import (
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
)

// StopHistoryObserver 安装已 Sync 的只读快照，查询 owner 不接触配置或执行器。
type StopHistoryObserver interface {
	StopHooksCommitted(session.ReplayedStop) error
	StopFeedbackCommitted([]uint64) error
}

func (r *Runtime) publishStop(turn domain.TurnID, state session.ReplayedStop, phase protocol.StopHookPhase) error {
	if state.Validate() != nil {
		r.poisoned.Store(true)
		return domain.HookConfigInvalid
	}
	if r.stopHistory != nil {
		if err := r.stopHistory.StopHooksCommitted(state.Clone()); err != nil {
			r.poisoned.Store(true)
			return err
		}
	}
	p := protocol.StopHookCommittedPayload{Target: state.Plan.Target(), PlanDigest: state.Plan.Digest(), DetailSequence: state.DetailSequence, Phase: phase}
	if state.Decision != nil {
		p.Disposition = state.Decision.Disposition
	}
	event, err := protocol.NewStopHookCommitted(p)
	if err != nil {
		r.poisoned.Store(true)
		return err
	}
	if r.turnEmitter != nil {
		r.turnEmitter(r.decorate(event, turn))
	}
	return nil
}
