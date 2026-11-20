package anthropic

import (
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

func prepareToolOutputsEntry(history []nativeHistoryEntry, results []tool.ModelOutput) (nativeHistoryEntry, error) {
	calls, err := pendingToolUses(history)
	if err != nil {
		return nativeHistoryEntry{}, err
	}
	return toolOutputsEntry(history[len(history)-1].TurnID, calls, results)
}

func toolOutputsEntry(turn domain.TurnID, calls []NativeItem, results []tool.ModelOutput) (nativeHistoryEntry, error) {
	items, err := provider.EncodeNativeToolOutputs(calls, results, "anthropic tool result count does not match pending calls", encodeToolResult)
	if err != nil {
		return nativeHistoryEntry{}, err
	}
	entry := nativeHistoryEntry{Kind: nativeHistoryToolOutputs, TurnID: turn, ToolOutputs: nativeMessage{Role: roleUser, Content: items}}
	if err := validateNativeHistoryEntry(entry); err != nil {
		return nativeHistoryEntry{}, err
	}
	return entry, nil
}

func nativeSequenceRules() provider.NativeSequenceRules[nativeHistoryEntry, NativeItem] {
	return provider.NativeSequenceRules[nativeHistoryEntry, NativeItem]{
		ValidateEntry: validateNativeHistoryEntry,
		Calls:         func(entry nativeHistoryEntry) []NativeItem { return toolUses(entry.Assistant.Content) },
		MatchOutputs: func(calls []NativeItem, entry nativeHistoryEntry) error {
			return matchToolResults(calls, entry.ToolOutputs.Content)
		},
		Feedback: entryStopFeedback,
	}
}

func validateNativeHistory(entries []nativeHistoryEntry) error {
	return provider.ValidateNativeSequence(entries, nativeSequenceRules())
}

const pendingCallsUnavailable = "anthropic native history has no pending tool uses"

func pendingToolUses(history []nativeHistoryEntry) ([]NativeItem, error) {
	return provider.PendingNativeCalls(history, nativeSequenceRules(), pendingCallsUnavailable)
}

func toolUses(blocks []NativeItem) []NativeItem {
	uses := make([]NativeItem, 0)
	for _, block := range blocks {
		if block.Type == blockTypeToolUse {
			uses = append(uses, block.clone())
		}
	}
	return uses
}

func matchToolResults(calls []NativeItem, results []NativeItem) error {
	if len(calls) != len(results) {
		return fmt.Errorf("anthropic tool result count does not match pending calls")
	}
	for index, result := range results {
		if result.Type != blockTypeToolResult || result.ToolUseID != calls[index].ID {
			return fmt.Errorf("anthropic tool result order does not match pending calls")
		}
		if calls[index].Name == "AskUserQuestion" && !result.IsError {
			input, err := tool.DecodeQuestionInput(calls[index].Input)
			if err != nil {
				return err
			}
			if _, err := tool.DecodeQuestionPreview(input, []byte(result.Content)); err != nil {
				return err
			}
		}
	}
	return nil
}
