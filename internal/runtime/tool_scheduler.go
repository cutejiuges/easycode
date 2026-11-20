package runtime

import (
	"context"
	"errors"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/tool"
)

// scheduleToolCalls 以只读批次和独占屏障调度同一 sample，按调用顺序提交结果。
func (runtime *Runtime) scheduleToolCalls(ctx context.Context, turnID domain.TurnID, invocations []tool.Invocation, emit Emitter) ([]tool.InvocationResult, error) {
	runtime.readObservations.beginGroup()
	results := make([]tool.InvocationResult, len(invocations))
	for index := 0; index < len(invocations); {
		invocation := invocations[index]
		if invocation.Capability().Exclusive() {
			result, aborted, err := runtime.executeExclusiveCall(ctx, emit, turnID, invocation)
			results[index] = result
			if err == nil && !aborted {
				index++
				continue
			}

			code, message := "cancelled", "Tool cancelled"
			if err == nil {
				code, message = "user_aborted", "Tool cancelled: user aborted tool approval"
				err = fault.Wrap(fault.CodeUserCancelled, "turn was aborted", context.Canceled)
			}
			call := runtime.toolTurn.toolCalls[invocation.InvocationID()]
			post := runtime.hooks.post[invocation.InvocationID()]
			// 前序尚未持久收口时，保留可恢复的前缀及触发失败的原始错误。
			if result.Validate() != nil || call == nil || !call.hasResult || (post != nil && post.terminal == nil) || runtime.journal.Poisoned() {
				return nil, err
			}
			for next := index + 1; next < len(invocations); next++ {
				results[next] = tool.NewInvocationErrorResult(invocations[next], tool.ResultCancelled, code, message)
				if persistErr := runtime.persistToolResult(context.WithoutCancel(ctx), turnID, &results[next]); persistErr != nil {
					return nil, errors.Join(err, persistErr)
				}
			}
			return results, err
		}
		end := index + 1
		for end < len(invocations) && !invocations[end].Capability().Exclusive() {
			end++
		}
		batch, err := runtime.executeReadBatch(ctx, turnID, invocations[index:end])
		if err != nil {
			return nil, err
		}
		copy(results[index:end], batch)
		index = end
	}
	return results, nil
}
