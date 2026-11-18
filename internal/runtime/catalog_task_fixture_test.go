package runtime

import (
	"context"
	"easycode/internal/tool"
)

// catalogTaskFixture 提供无外部副作用的真实任务结果消费者。
type catalogTaskFixture struct{}

func (catalogTaskFixture) Execute(_ context.Context, inv tool.TaskInvocation) tool.InvocationResult {
	result, _ := tool.NewTaskInvocationResult(inv)
	return result
}
