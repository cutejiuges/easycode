package openai

import (
	"strings"
	"testing"

	"easycode/internal/tool"
)

func TestPrepareToolOutputsPairsResultsInModelOrder(t *testing.T) {
	input, err := tool.NewReadInput("README.md", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	callID, err := tool.ParseProviderCallID("call-1")
	if err != nil {
		t.Fatal(err)
	}
	ready, err := tool.NewReadReadyCall(callID, input)
	if err != nil {
		t.Fatal(err)
	}
	invocationID, err := tool.GenerateInvocationID()
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := tool.NewReadInvocation(invocationID, ready)
	if err != nil {
		t.Fatal(err)
	}
	result := tool.NewReadErrorResult(invocation, tool.ResultError, "read_failed", "cannot read file", ".")
	history := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, NewUserItem("read it")), {
		Kind: nativeHistorySample,
		Outputs: []NativeItem{{
			Type: "function_call", ID: "fc-1", CallID: "call-1", Name: "Read", Arguments: `{"file_path":"README.md"}`,
		}},
	}})
	entry, err := prepareToolOutputsEntry(history, testModelOutputs(t, history, result))
	if err != nil {
		t.Fatal(err)
	}
	if entry.Kind != nativeHistoryToolOutputs || len(entry.ToolOutputs) != 1 ||
		entry.ToolOutputs[0].CallID != "call-1" || !strings.Contains(entry.ToolOutputs[0].Output, `"status":"error"`) {
		t.Fatalf("tool output entry = %#v", entry)
	}
	if err := validateNativeHistory(append(history, entry)); err != nil {
		t.Fatalf("history is invalid: %v", err)
	}
}
