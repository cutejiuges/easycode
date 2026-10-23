package runtime

import (
	"context"
	"easycode/internal/tool"
)

type catalogPatchFixture struct{}

func (catalogPatchFixture) Prepare(context.Context, tool.PatchInvocation) (tool.PatchCandidate, error) {
	return tool.PatchCandidate{}, nil
}
func (catalogPatchFixture) Execute(context.Context, tool.PatchCandidate) tool.InvocationResult {
	return tool.InvocationResult{}
}

type catalogExecFixture struct{}

func (catalogExecFixture) Prepare(tool.ExecInvocation) (tool.ExecCandidate, error) {
	return tool.ExecCandidate{}, nil
}
func (catalogExecFixture) Execute(context.Context, tool.ExecCandidate, func(tool.ExecProgress)) tool.InvocationResult {
	return tool.InvocationResult{}
}
