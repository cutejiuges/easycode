package tool

import (
	"context"
)

type catalogPatchFixture struct{}

func (catalogPatchFixture) Prepare(context.Context, PatchInvocation) (PatchCandidate, error) {
	return PatchCandidate{}, nil
}
func (catalogPatchFixture) Execute(context.Context, PatchCandidate) InvocationResult {
	return InvocationResult{}
}

type catalogExecFixture struct{}

func (catalogExecFixture) Prepare(ExecInvocation) (ExecCandidate, error) { return ExecCandidate{}, nil }
func (catalogExecFixture) Execute(context.Context, ExecCandidate, func(ExecProgress)) InvocationResult {
	return InvocationResult{}
}
