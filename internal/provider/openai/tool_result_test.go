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
	ready, err := tool.NewReadyCall(callID, input)
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
	history := []nativeHistoryEntry{{
		Kind: nativeHistorySample, Input: nativeItemPointer(NewUserItem("read it")),
		Outputs: []NativeItem{{
			Type: "function_call", ID: "fc-1", CallID: "call-1", Name: "Read", Arguments: `{"file_path":"README.md"}`,
		}},
	}}
	entry, err := prepareToolOutputsEntry(history, []tool.InvocationResult{result})
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

func TestPrepareToolOutputsRejectsMismatchedCallID(t *testing.T) {
	input, _ := tool.NewReadInput("README.md", 1, 20)
	callID, _ := tool.ParseProviderCallID("different-call")
	ready, _ := tool.NewReadyCall(callID, input)
	invocationID, _ := tool.GenerateInvocationID()
	invocation, _ := tool.NewReadInvocation(invocationID, ready)
	result := tool.NewReadErrorResult(invocation, tool.ResultError, "read_failed", "cannot read file", ".")
	history := []nativeHistoryEntry{{
		Kind: nativeHistorySample, Input: nativeItemPointer(NewUserItem("read it")),
		Outputs: []NativeItem{{
			Type: "function_call", ID: "fc-1", CallID: "call-1", Name: "Read", Arguments: `{"file_path":"README.md"}`,
		}},
	}}
	if _, err := prepareToolOutputsEntry(history, []tool.InvocationResult{result}); err == nil {
		t.Fatal("mismatched tool result unexpectedly accepted")
	}
}
