package runtime

import (
	"context"

	"easycode/internal/tool"
)

type postPatchOutcome struct{ status tool.ResultStatus }

func (e postPatchOutcome) Execute(_ context.Context, c tool.PatchCandidate) tool.InvocationResult {
	ready, _ := tool.NewPatchReadyCall(c.ProviderCallID(), c.WireKind(), c.Input())
	invocation, _ := tool.NewPatchInvocation(c.InvocationID(), ready)
	return tool.NewPatchErrorResult(invocation, e.status, "fixture_outcome", "Tool did not succeed")
}
