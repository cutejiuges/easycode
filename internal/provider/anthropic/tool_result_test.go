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
	history := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, newUserMessage("read it")), {
		Kind: nativeHistorySample,
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{
			Type: blockTypeToolUse, ID: "toolu-1", Name: "Read", Input: []byte(`{"file_path":"README.md"}`),
		}}},
		Metadata: messageMetadata{ID: "msg-1", Model: "claude-test"},
	}})
	entry, err := prepareToolOutputsEntry(history, testModelOutputs(t, history, result))
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
