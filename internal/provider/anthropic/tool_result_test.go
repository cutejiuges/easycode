package anthropic

import (
	"testing"

	"easycode/internal/tool"
)

func TestPrepareToolOutputsPairsResultsInModelOrder(t *testing.T) {
	input, _ := tool.NewReadInput("README.md", 1, 20)
	callID, _ := tool.ParseProviderCallID("toolu-1")
	ready, _ := tool.NewReadReadyCall(callID, input)
	invocationID, _ := tool.GenerateInvocationID()
	invocation, _ := tool.NewReadInvocation(invocationID, ready)
	result := tool.NewReadErrorResult(invocation, tool.ResultError, "read_failed", "cannot read file", ".")
	history := []nativeHistoryEntry{{
		Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("read it")),
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{
			Type: blockTypeToolUse, ID: "toolu-1", Name: "Read", Input: []byte(`{"file_path":"README.md"}`),
		}}},
		Metadata: messageMetadata{ID: "msg-1", Model: "claude-test"},
	}}
	entry, err := prepareToolOutputsEntry(history, []tool.InvocationResult{result})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Kind != nativeHistoryToolOutputs || entry.ToolOutputs.Role != roleUser ||
		len(entry.ToolOutputs.Content) != 1 || entry.ToolOutputs.Content[0].ToolUseID != "toolu-1" ||
		entry.ToolOutputs.Content[0].Content != "cannot read file" || !entry.ToolOutputs.Content[0].IsError {
		t.Fatalf("tool outputs = %#v", entry)
	}
	if err := validateNativeHistory(append(history, entry)); err != nil {
		t.Fatalf("history is invalid: %v", err)
	}
}

func TestPreparePatchToolOutputPreservesEditPairing(t *testing.T) {
	replacement, _ := tool.NewPatchReplacement("old", "new", false)
	input, _ := tool.NewPatchInput("main.go", []tool.PatchReplacement{replacement})
	callID, _ := tool.ParseProviderCallID("toolu-edit")
	ready, _ := tool.NewPatchReadyCall(callID, tool.WireStructured, input)
	invocationID, _ := tool.GenerateInvocationID()
	invocation, _ := tool.NewPatchInvocation(invocationID, ready)
	result := tool.NewPatchErrorResult(invocation, tool.ResultDenied, "approval_required", "Patch denied: approval_required")
	history := []nativeHistoryEntry{{Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("edit it")), Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeToolUse, ID: "toolu-edit", Name: "Edit", Input: []byte(`{"file_path":"main.go","old_string":"old","new_string":"new"}`)}}}, Metadata: messageMetadata{ID: "msg-edit", Model: "claude-test"}}}
	entry, err := prepareToolOutputsEntry(history, []tool.InvocationResult{result})
	if err != nil {
		t.Fatal(err)
	}
	block := entry.ToolOutputs.Content[0]
	if block.Type != blockTypeToolResult || block.ToolUseID != "toolu-edit" || !block.IsError || block.Content != result.Preview().Text() {
		t.Fatalf("patch output = %#v", block)
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
		Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("read it")),
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{
			Type: blockTypeToolUse, ID: "toolu-1", Name: "Read", Input: []byte(`{"file_path":"README.md"}`),
		}}},
		Metadata: messageMetadata{ID: "msg-1", Model: "claude-test"},
	}}
	if _, err := prepareToolOutputsEntry(history, []tool.InvocationResult{result}); err == nil {
		t.Fatal("mismatched tool result unexpectedly accepted")
	}
}

func TestPrepareToolOutputsRejectsCapabilityMismatch(t *testing.T) {
	input, _ := tool.NewGlobInput("**/*.go", "", 10)
	callID, _ := tool.ParseProviderCallID("toolu-1")
	ready, _ := tool.NewGlobReadyCall(callID, input)
	invocationID, _ := tool.GenerateInvocationID()
	invocation, _ := tool.NewGlobInvocation(invocationID, ready)
	metadata, _ := tool.NewGlobResultMetadata([]string{"main.go"}, false, 0, 1, tool.SearchComplete, tool.SearchSkipCounts{})
	preview, _ := tool.NewModelPreview("main.go\n")
	result, _ := tool.NewGlobInvocationResult(invocation, tool.ResultSuccess, "ok", preview, metadata)
	history := []nativeHistoryEntry{{
		Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("read it")),
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{
			Type: blockTypeToolUse, ID: "toolu-1", Name: "Read", Input: []byte(`{"file_path":"README.md"}`),
		}}},
		Metadata: messageMetadata{ID: "msg-1", Model: "claude-test"},
	}}
	if _, err := prepareToolOutputsEntry(history, []tool.InvocationResult{result}); err == nil {
		t.Fatal("capability 与 pending call 错配未被拒绝")
	}
}
