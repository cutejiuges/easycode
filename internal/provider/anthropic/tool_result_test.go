package anthropic

import (
	"testing"

	"easycode/internal/tool"
)

func TestPrepareToolOutputsPairsResultsInModelOrder(t *testing.T) {
	input, _ := tool.NewReadInput("README.md", 1, 20)
	callID, _ := tool.ParseProviderCallID("toolu-1")
	ready, _ := tool.NewReadyCall(callID, input)
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

func TestPrepareToolOutputsRejectsMismatchedCallID(t *testing.T) {
	input, _ := tool.NewReadInput("README.md", 1, 20)
	callID, _ := tool.ParseProviderCallID("different-call")
	ready, _ := tool.NewReadyCall(callID, input)
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
