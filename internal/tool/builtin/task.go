package builtin

import (
	"context"

	"easycode/internal/tool"
)

// TaskExecutor 仅计算完整结果，任务应用由 durable 提交 owner 决定。
type TaskExecutor struct{}

// NewTaskExecutor 纯内存创建不持有状态或后台工作的执行器。
func NewTaskExecutor() TaskExecutor { return TaskExecutor{} }

func (TaskExecutor) Execute(ctx context.Context, v tool.TaskInvocation) tool.InvocationResult {
	if v.Validate() != nil || ctx == nil {
		return tool.InvocationResult{}
	}
	if ctx.Err() != nil {
		call, _ := tool.NewTaskReadyCall(v.ProviderCallID(), v.Input())
		inv, _ := tool.NewInvocation(v.InvocationID(), call)
		return tool.NewInvocationErrorResult(inv, tool.ResultCancelled, "cancelled", "Task update cancelled")
	}
	result, _ := tool.NewTaskInvocationResult(v)
	return result
}
