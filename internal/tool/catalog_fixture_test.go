package tool

import (
	"context"
)

type catalogExecFixture struct{}

func (catalogExecFixture) Prepare(ExecInvocation) (ExecCandidate, error) { return ExecCandidate{}, nil }
func (catalogExecFixture) Execute(context.Context, ExecCandidate, func(ExecProgress)) InvocationResult {
	return InvocationResult{}
}
