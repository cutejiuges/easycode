package builtin

import (
	"context"
	"errors"

	"easycode/internal/tool"
)

// QuestionExecutor 只转换已接收答案，不持有终端或后台任务。
type QuestionExecutor struct{}

// NewQuestionExecutor 纯内存创建提问执行器。
func NewQuestionExecutor() QuestionExecutor { return QuestionExecutor{} }

func (QuestionExecutor) Execute(ctx context.Context, v tool.QuestionInvocation, responder tool.QuestionResponder) tool.InvocationResult {
	call, err := tool.NewQuestionReadyCall(v.ProviderCallID(), v.Input())
	if err != nil {
		return tool.InvocationResult{}
	}
	inv, err := tool.NewInvocation(v.InvocationID(), call)
	if err != nil {
		return tool.InvocationResult{}
	}
	if responder == nil {
		return tool.NewInvocationErrorResult(inv, tool.ResultDenied, "user_input_unavailable", "Interactive user input is unavailable")
	}
	a, err := responder.Answer(ctx, v)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return tool.NewInvocationErrorResult(inv, tool.ResultCancelled, "cancelled", "Question cancelled")
		}
		return tool.NewInvocationErrorResult(inv, tool.ResultError, "user_input_failed", "User input failed")
	}
	r, err := tool.NewQuestionInvocationResult(v, a)
	if err != nil {
		return tool.InvocationResult{}
	}
	return r
}
