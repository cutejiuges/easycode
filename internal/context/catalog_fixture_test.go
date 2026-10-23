package context

import (
	"context"
	"easycode/internal/tool"
)

type catalogExecFixture struct{}

func (catalogExecFixture) Prepare(tool.ExecInvocation) (tool.ExecCandidate, error) {
	return tool.ExecCandidate{}, nil
}
func (catalogExecFixture) Execute(context.Context, tool.ExecCandidate, func(tool.ExecProgress)) tool.InvocationResult {
	return tool.InvocationResult{}
}
