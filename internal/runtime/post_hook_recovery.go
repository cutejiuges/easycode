package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/session"
	"easycode/internal/tool"
)

// reconcilePostHooks 只重用 durable 前缀，不读取配置、artifact 或进程状态。
func (r *Runtime) reconcilePostHooks(ctx context.Context, turn domain.TurnID, call session.ReplayedToolCall, result tool.InvocationResult) (bool, error) {
	if call.PostHooks == nil {
		return false, nil
	}
	g := call.PostHooks
	state := &postHookCall{plan: g.Plan.Clone(), finished: append([]domain.HookFinished(nil), g.Finished...), history: g.Clone()}
	if r.hooks.post == nil {
		r.hooks.post = make(map[tool.InvocationID]*postHookCall)
	}
	r.hooks.post[call.Invocation.InvocationID()] = state
	if g.Terminal != nil {
		terminal := g.Terminal.Clone()
		state.terminal = &terminal
		return terminal.Disposition == domain.PostHookUncertain, nil
	}
	disposition, code := domain.PostHookSkipped, domain.HookToolNotSuccessful
	var prefix []session.RecordDraft
	if result.Status() == tool.ResultSuccess {
		disposition, code = domain.PostHookCancelled, domain.HookPostInterrupted
		if g.Started != nil {
			disposition, code = domain.PostHookUncertain, domain.HookOutcomeUncertain
			stats := domain.HookRunResult{Identity: *g.Started, Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain, Origin: "recovery"}
			frozen, err := domain.NewEmptyHookFinished(stats, g.Plan.Plan.Hooks[g.Started.Ordinal])
			if err != nil {
				return false, err
			}
			finished, err := session.NewHookExecutionFinishedDraft(turn, frozen)
			if err != nil {
				return false, err
			}
			state.finished = append(state.finished, frozen)
			prefix = append(prefix, finished)
		}
	}
	err := r.completePostHooks(ctx, turn, result, state, disposition, code, "recovery", prefix)
	return disposition == domain.PostHookUncertain, err
}
