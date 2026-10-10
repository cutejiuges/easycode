package anthropic

import (
	"fmt"

	"easycode/internal/tool"
)

func prepareToolOutputsEntry(history []nativeHistoryEntry, results []tool.InvocationResult) (nativeHistoryEntry, error) {
	calls, err := pendingToolUses(history)
	if err != nil {
		return nativeHistoryEntry{}, err
	}
	if len(results) != len(calls) {
		return nativeHistoryEntry{}, fmt.Errorf("anthropic tool result count does not match pending calls")
	}
	blocks := make([]NativeItem, len(results))
	for index, result := range results {
		if err := result.Validate(); err != nil {
			return nativeHistoryEntry{}, fmt.Errorf("anthropic tool result %d is invalid: %w", index, err)
		}
		if string(result.ProviderCallID()) != calls[index].ID {
			return nativeHistoryEntry{}, fmt.Errorf("anthropic tool result order does not match pending calls")
		}
		if result.Capability() != anthropicCapabilityForName(calls[index].Name) {
			return nativeHistoryEntry{}, fmt.Errorf("anthropic tool result capability does not match pending call")
		}
		blocks[index] = NativeItem{
			Type: blockTypeToolResult, ToolUseID: calls[index].ID,
			Content: result.Preview().Text(), IsError: result.Status() != tool.ResultSuccess,
		}
	}
	entry := nativeHistoryEntry{
		Kind:        nativeHistoryToolOutputs,
		ToolOutputs: nativeMessage{Role: roleUser, Content: blocks},
	}
	if err := validateNativeHistoryEntry(entry); err != nil {
		return nativeHistoryEntry{}, err
	}
	return entry, nil
}

func anthropicCapabilityForName(name string) tool.CapabilityID {
	switch name {
	case "Read":
		return tool.CapabilityRead
	case "Glob":
		return tool.CapabilityGlob
	case "Grep":
		return tool.CapabilityGrep
	default:
		return ""
	}
}

func validateNativeHistory(entries []nativeHistoryEntry) error {
	var pending []NativeItem
	expectContinuation := false
	for index, entry := range entries {
		if err := validateNativeHistoryEntry(entry); err != nil {
			return fmt.Errorf("entry %d is invalid: %w", index+1, err)
		}
		switch entry.Kind {
		case nativeHistorySample:
			if len(pending) != 0 {
				return fmt.Errorf("anthropic sample appears before pending tool uses are closed")
			}
			if expectContinuation {
				expectContinuation = false
			} else if entry.Input == nil {
				return fmt.Errorf("anthropic sample input is required at this history position")
			}
			pending = toolUses(entry.Assistant.Content)
		case nativeHistoryToolOutputs:
			if len(pending) == 0 {
				return fmt.Errorf("anthropic tool outputs have no pending tool uses")
			}
			if err := matchToolResults(pending, entry.ToolOutputs.Content); err != nil {
				return err
			}
			pending = nil
			expectContinuation = true
		}
	}
	return nil
}

func pendingToolUses(history []nativeHistoryEntry) ([]NativeItem, error) {
	if err := validateNativeHistory(history); err != nil {
		return nil, err
	}
	if len(history) == 0 || history[len(history)-1].Kind != nativeHistorySample {
		return nil, fmt.Errorf("anthropic native history has no pending tool uses")
	}
	calls := toolUses(history[len(history)-1].Assistant.Content)
	if len(calls) == 0 {
		return nil, fmt.Errorf("anthropic native history has no pending tool uses")
	}
	return calls, nil
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
	}
	return nil
}
