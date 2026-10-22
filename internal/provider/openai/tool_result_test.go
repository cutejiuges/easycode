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

func TestPrepareCustomPatchOutputPreservesWireKind(t *testing.T) {
	replacement, _ := tool.NewPatchReplacement("old", "new", false)
	input, _ := tool.NewPatchInput("main.go", []tool.PatchReplacement{replacement})
	callID, _ := tool.ParseProviderCallID("call-patch")
	ready, _ := tool.NewPatchReadyCall(callID, tool.WireCustomFreeform, input)
	invocationID, _ := tool.GenerateInvocationID()
	invocation, _ := tool.NewPatchInvocation(invocationID, ready)
	result := tool.NewPatchErrorResult(invocation, tool.ResultDenied, "approval_required", "Patch denied: approval_required")
	patch := "*** Begin Patch\n*** Update File: main.go\n@@\n-old\n+new\n*** End Patch\n"
	history := []nativeHistoryEntry{{Kind: nativeHistorySample, Input: nativeItemPointer(NewUserItem("edit it")), Outputs: []NativeItem{{Type: "custom_tool_call", ID: "ctc-1", CallID: "call-patch", Name: "apply_patch", Input: patch}}}}
	entry, err := prepareToolOutputsEntry(history, []tool.InvocationResult{result})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.ToolOutputs) != 1 || entry.ToolOutputs[0].Type != "custom_tool_call_output" || entry.ToolOutputs[0].CallID != "call-patch" || !strings.Contains(entry.ToolOutputs[0].Output, `"status":"denied"`) {
		t.Fatalf("output=%#v", entry.ToolOutputs)
	}
	if err := validateNativeHistory(append(history, entry)); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareToolOutputsRejectsMismatchedCallID(t *testing.T) {
	input, _ := tool.NewReadInput("README.md", 1, 20)
	callID, _ := tool.ParseProviderCallID("different-call")
	ready, _ := tool.NewReadReadyCall(callID, input)
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

func TestPrepareToolOutputsAcceptsOrderedHeterogeneousResultsAndRejectsCapabilityMismatch(t *testing.T) {
	history := []nativeHistoryEntry{{
		Kind: nativeHistorySample, Input: nativeItemPointer(NewUserItem("search")),
		Outputs: []NativeItem{
			{Type: "function_call", ID: "fc-0", CallID: "call-0", Name: "Glob", Arguments: `{"pattern":"**/*.go"}`},
			{Type: "function_call", ID: "fc-1", CallID: "call-1", Name: "Grep", Arguments: `{"pattern":"TODO"}`},
			{Type: "function_call", ID: "fc-2", CallID: "call-2", Name: "Read", Arguments: `{"file_path":"README.md"}`},
		},
	}}
	results := []tool.InvocationResult{
		testGlobProviderResult(t, "call-0"),
		testGrepProviderResult(t, "call-1"),
		testReadProviderResult(t, "call-2"),
	}
	entry, err := prepareToolOutputsEntry(history, results)
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.ToolOutputs) != 3 || entry.ToolOutputs[0].CallID != "call-0" || entry.ToolOutputs[1].CallID != "call-1" || entry.ToolOutputs[2].CallID != "call-2" {
		t.Fatalf("异构 outputs 顺序错误: %#v", entry.ToolOutputs)
	}
	mismatched := append([]tool.InvocationResult(nil), results...)
	mismatched[0] = testReadProviderResult(t, "call-0")
	if _, err := prepareToolOutputsEntry(history, mismatched); err == nil {
		t.Fatal("capability 与 pending call 错配未被拒绝")
	}
}

func testGlobProviderResult(t *testing.T, call string) tool.InvocationResult {
	t.Helper()
	input, _ := tool.NewGlobInput("**/*.go", "", 10)
	callID, _ := tool.ParseProviderCallID(call)
	ready, _ := tool.NewGlobReadyCall(callID, input)
	invocationID, _ := tool.GenerateInvocationID()
	invocation, _ := tool.NewGlobInvocation(invocationID, ready)
	metadata, _ := tool.NewGlobResultMetadata([]string{"main.go"}, false, 0, 1, tool.SearchComplete, tool.SearchSkipCounts{})
	preview, _ := tool.NewModelPreview("main.go\n")
	result, err := tool.NewGlobInvocationResult(invocation, tool.ResultSuccess, "ok", preview, metadata)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func testGrepProviderResult(t *testing.T, call string) tool.InvocationResult {
	t.Helper()
	input, _ := tool.NewGrepInput("TODO", "", "", tool.GrepOutputFilesWithMatches, false, 0, 0, 10)
	callID, _ := tool.ParseProviderCallID(call)
	ready, _ := tool.NewGrepReadyCall(callID, input)
	invocationID, _ := tool.GenerateInvocationID()
	invocation, _ := tool.NewGrepInvocation(invocationID, ready)
	match, _ := tool.NewGrepFileMatch("main.go")
	metadata, _ := tool.NewGrepResultMetadata(tool.GrepOutputFilesWithMatches, []tool.GrepMatch{match}, 1, false, 0, 1, 1, 10, tool.SearchComplete, tool.SearchSkipCounts{})
	preview, _ := tool.NewModelPreview("main.go\n")
	result, err := tool.NewGrepInvocationResult(invocation, tool.ResultSuccess, "ok", preview, metadata)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func testReadProviderResult(t *testing.T, call string) tool.InvocationResult {
	t.Helper()
	input, _ := tool.NewReadInput("README.md", 1, 20)
	callID, _ := tool.ParseProviderCallID(call)
	ready, _ := tool.NewReadReadyCall(callID, input)
	invocationID, _ := tool.GenerateInvocationID()
	invocation, _ := tool.NewReadInvocation(invocationID, ready)
	return tool.NewReadErrorResult(invocation, tool.ResultError, "read_failed", "cannot read file", ".")
}
