package context

import (
	"context"
	"easycode/internal/tool"
)

// catalogQuestionFixture 为既有目录测试提供显式组件，不改变其测试目标。
type catalogQuestionFixture struct{}

func (catalogQuestionFixture) Execute(context.Context, tool.QuestionInvocation, tool.QuestionResponder) tool.InvocationResult {
	return tool.InvocationResult{}
}
