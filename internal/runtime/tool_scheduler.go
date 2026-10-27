package runtime

import (
	"context"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/tool"
)

// orderedToolScheduler 以只读批次和独占屏障调度同一 sample，按调用顺序提交结果。
type toolScheduleOperations interface {
	executeApproved(context.Context, Emitter, domain.TurnID, tool.Invocation) (tool.InvocationResult, bool, error)
	executeReadBatch(context.Context, domain.TurnID, []tool.Invocation) ([]tool.InvocationResult, error)
	persistToolResult(context.Context, domain.TurnID, tool.InvocationResult) error
}

type orderedToolScheduler struct {
	operations   toolScheduleOperations
	observations *readObservations
}

func (scheduler orderedToolScheduler) execute(ctx context.Context, turnID domain.TurnID, invocations []tool.Invocation, emitters ...Emitter) ([]tool.InvocationResult, error) {
	emit := Emitter(func(protocol.Event) {})
	if len(emitters) == 1 && emitters[0] != nil {
		emit = emitters[0]
	}
	operations := scheduler.operations
	scheduler.observations.beginGroup()
	results := make([]tool.InvocationResult, len(invocations))
	for index := 0; index < len(invocations); {
		if invocations[index].Capability().Exclusive() {
			result, aborted, err := operations.executeApproved(ctx, emit, turnID, invocations[index])
			if err != nil {
				if result.Validate() != nil {
					return nil, err
				}
				results[index] = result
				for next := index + 1; next < len(invocations); next++ {
					results[next] = tool.NewInvocationErrorResult(invocations[next], tool.ResultCancelled, "cancelled", "Tool cancelled")
					if persistErr := operations.persistToolResult(context.WithoutCancel(ctx), turnID, results[next]); persistErr != nil {
						return nil, persistErr
					}
				}
				return results, err
			}
			results[index] = result
			if aborted {
				for next := index + 1; next < len(invocations); next++ {
					results[next] = tool.NewInvocationErrorResult(invocations[next], tool.ResultCancelled, "user_aborted", "Tool cancelled: user aborted tool approval")
					if err := operations.persistToolResult(context.WithoutCancel(ctx), turnID, results[next]); err != nil {
						return nil, err
					}
				}
				return results, fault.Wrap(fault.CodeUserCancelled, "turn was aborted", context.Canceled)
			}
			index++
			continue
		}
		end := index + 1
		for end < len(invocations) && !invocations[end].Capability().Exclusive() {
			end++
		}
		batch, err := operations.executeReadBatch(ctx, turnID, invocations[index:end])
		if err != nil {
			return nil, err
		}
		copy(results[index:end], batch)
		index = end
	}
	return results, nil
}
