package anthropic

import (
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider/testsupport"
	"easycode/internal/tool"
)

func testNativeInvocation(t *testing.T, item NativeItem, id tool.InvocationID, callID tool.ProviderCallID) tool.Invocation {
	t.Helper()
	ready, err := testsupport.ToolCatalog(t).DecodeReadyCall(domain.ProviderAnthropic, item.Name, callID, item.Input)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := tool.NewInvocation(id, ready)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}
func testModelOutputs(t *testing.T, history []nativeHistoryEntry, results ...tool.InvocationResult) []tool.ModelOutput {
	t.Helper()
	calls, err := pendingToolUses(history)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]tool.ModelOutput, len(results))
	for i, result := range results {
		if i >= len(calls) {
			t.Fatal("missing native fixture input")
		}
		invocation := testNativeInvocation(t, calls[i], result.InvocationID(), result.ProviderCallID())
		out[i] = testsupport.DirectModelOutput(t, invocation, result, tool.ExecutionAdmission{})
	}
	return out
}
