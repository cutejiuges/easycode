package runtime

import (
	"context"

	"easycode/internal/domain"
	"easycode/internal/session"
	"easycode/internal/tool"
)

// reconcileHookGate 只追加冻结计划的本地补偿，不读取配置、探测 PID 或运行脚本。
func (r *Runtime) reconcileHookGate(ctx context.Context, turn domain.TurnID, call session.ReplayedToolCall) (tool.InvocationResult, error) {
	g := call.Hooks
	status, code := domain.HookCancel, domain.HookInterrupted
	completed := len(g.Results)
	blocking := min(completed, len(g.Plan.Hooks)-1)
	var prefix []session.RecordDraft
	if g.Started != nil {
		status, code = domain.HookUncertain, domain.HookOutcomeUncertain
		completed++
		blocking = g.Started.Ordinal
		result := domain.HookRunResult{Identity: *g.Started, Status: status, Code: code, Origin: "recovery"}
		frozen, err := domain.NewEmptyHookFinished(result, g.Plan.Hooks[result.Identity.Ordinal])
		if err != nil {
			return tool.InvocationResult{}, err
		}
		draft, err := session.NewHookExecutionFinishedDraft(turn, frozen)
		if err != nil {
			return tool.InvocationResult{}, err
		}
		prefix = append(prefix, draft)
	}
	return r.writeHookGateResult(ctx, turn, call.Invocation, g.Plan, status, code, completed, blocking, prefix, true)
}
