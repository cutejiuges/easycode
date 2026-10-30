package anthropic

import (
	"easycode/internal/domain"
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
		if result.WireKind() != tool.WireStructured || result.Capability() != anthropicCapabilityForName(calls[index].Name) {
			return nativeHistoryEntry{}, fmt.Errorf("anthropic tool result capability does not match pending call")
		}
		blocks[index] = NativeItem{
			Type: blockTypeToolResult, ToolUseID: calls[index].ID,
			Content: result.Preview().Text(), IsError: result.Status() != tool.ResultSuccess,
		}
	}
	entry := nativeHistoryEntry{
		Kind: nativeHistoryToolOutputs, TurnID: history[len(history)-1].TurnID,
		ToolOutputs: nativeMessage{Role: roleUser, Content: blocks},
	}
	if err := validateNativeHistoryEntry(entry); err != nil {
		return nativeHistoryEntry{}, err
	}
	return entry, nil
}

func anthropicCapabilityForName(name string) tool.CapabilityID {
	switch name {
	case "Bash":
		return tool.CapabilityExec
	case "ReadArtifact":
		return tool.CapabilityReadArtifact
	case "Read":
		return tool.CapabilityRead
	case "Glob":
		return tool.CapabilityGlob
	case "Write":
		return tool.CapabilityWrite
	case "Grep":
		return tool.CapabilityGrep
	case "Edit":
		return tool.CapabilityPatch
	default:
		return ""
	}
}

func validateNativeHistory(entries []nativeHistoryEntry) error {
	var pending []NativeItem
	var turnID domain.TurnID
	var nextSample uint32
	sampleAllowed := false
	steeringAllowed := false
	seenInputs := make(map[domain.InputID]struct{})
	seenTurns := make(map[domain.TurnID]struct{})
	for index, entry := range entries {
		if err := validateNativeHistoryEntry(entry); err != nil {
			return fmt.Errorf("entry %d is invalid: %w", index+1, err)
		}
		switch entry.Kind {
		case nativeHistoryUserInputs:
			if len(pending) != 0 {
				return fmt.Errorf("user inputs precede pending tool outputs")
			}
			a, err := entry.InputMetadata.Association()
			if err != nil {
				return err
			}
			if entry.TurnID != a.TurnID() {
				return fmt.Errorf("input turn association mismatch")
			}
			if a.Placement() == domain.InputInitial {
				if _, exists := seenTurns[a.TurnID()]; exists {
					return fmt.Errorf("initial input is duplicated")
				}
				seenTurns[a.TurnID()] = struct{}{}
				turnID = a.TurnID()
				nextSample = 0
			} else if !steeringAllowed || turnID != a.TurnID() || nextSample != a.SampleIndex() {
				return fmt.Errorf("steering input position is invalid")
			}
			for _, id := range a.IDs() {
				if _, exists := seenInputs[id]; exists {
					return fmt.Errorf("input ID is duplicated")
				}
				seenInputs[id] = struct{}{}
			}
			sampleAllowed = true
			steeringAllowed = false
		case nativeHistorySample:
			if len(pending) != 0 || !sampleAllowed || entry.TurnID != turnID {
				return fmt.Errorf("sample history position is invalid")
			}
			sampleAllowed = false
			steeringAllowed = false
			nextSample++
			pending = toolUses(entry.Assistant.Content)
		case nativeHistoryToolOutputs:
			if len(pending) == 0 || entry.TurnID != turnID {
				return fmt.Errorf("tool outputs have no matching pending calls")
			}
			if err := matchToolResults(pending, entry.ToolOutputs.Content); err != nil {
				return err
			}
			pending = nil
			sampleAllowed = true
			steeringAllowed = true
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
