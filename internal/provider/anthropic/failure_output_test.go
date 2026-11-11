package anthropic

import (
	"easycode/internal/tool"
	"testing"
)

func postFailureProviderResult(t *testing.T, r tool.InvocationResult) tool.InvocationResult {
	return postFailureProviderResultWithCode(t, r, "execution_failed")
}
func postFailureProviderResultWithCode(t *testing.T, r tool.InvocationResult, code string) tool.InvocationResult {
	t.Helper()
	var result tool.InvocationResult
	switch r.Capability() {
	case tool.CapabilityPatch:
		replacement, _ := tool.NewPatchReplacement("old", "new", false)
		input, _ := tool.NewPatchInput("main.go", []tool.PatchReplacement{replacement})
		ready, _ := tool.NewPatchReadyCall(r.ProviderCallID(), r.WireKind(), input)
		v, _ := tool.NewPatchInvocation(r.InvocationID(), ready)
		result = tool.NewPatchErrorResult(v, tool.ResultError, code, "Tool execution failed")
	case tool.CapabilityWrite:
		input, _ := tool.NewWriteInput("written", "new")
		ready, _ := tool.NewWriteReadyCall(r.ProviderCallID(), input)
		v, _ := tool.NewWriteInvocation(r.InvocationID(), ready)
		result = tool.NewWriteErrorResult(v, tool.ResultError, code, "Tool execution failed")
	case tool.CapabilityExec:
		input, _ := tool.NewExecInput("exit 7", "", 1000)
		ready, _ := tool.NewExecReadyCall(r.ProviderCallID(), input)
		v, _ := tool.NewExecInvocation(r.InvocationID(), ready)
		result = tool.NewExecErrorResult(v, tool.ResultError, code, "Tool execution failed")
	}
	result, err := tool.FinalizeResult(result, 240<<10, tool.BudgetPreserved)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
