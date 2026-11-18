package anthropic

import (
	"context"
	"easycode/internal/tool"
)

// catalogQuestionFixture 为既有目录测试提供显式组件，不改变其测试目标。
type catalogQuestionFixture struct{}

func (catalogQuestionFixture) Execute(context.Context, tool.QuestionInvocation, tool.QuestionResponder) tool.InvocationResult {
	return tool.InvocationResult{}
}

// catalogTaskFixture 为目录提供真实的纯内存任务消费者。
type catalogTaskFixture struct{}

func (catalogTaskFixture) Execute(_ context.Context, invocation tool.TaskInvocation) tool.InvocationResult {
	result, _ := tool.NewTaskInvocationResult(invocation)
	return result
}
