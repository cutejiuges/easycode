package openai

import (
	"easycode/internal/domain"
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/tool"
)

type functionOutputPayload struct {
	Status  tool.ResultStatus `json:"status"`
	Code    string            `json:"code"`
	Content string            `json:"content"`
}

func prepareToolOutputsEntry(history []nativeHistoryEntry, results []tool.InvocationResult) (nativeHistoryEntry, error) {
	calls, err := pendingFunctionCalls(history)
	if err != nil {
		return nativeHistoryEntry{}, err
	}
	if len(results) != len(calls) {
		return nativeHistoryEntry{}, fmt.Errorf("OpenAI tool result count does not match pending calls")
	}
	outputs := make([]NativeItem, len(results))
	for index, result := range results {
		if err := result.Validate(); err != nil {
			return nativeHistoryEntry{}, fmt.Errorf("OpenAI tool result %d is invalid: %w", index, err)
		}
		if string(result.ProviderCallID()) != calls[index].CallID {
			return nativeHistoryEntry{}, fmt.Errorf("OpenAI tool result order does not match pending calls")
		}
		if result.Capability() != openAICapabilityForName(calls[index].Name) {
			return nativeHistoryEntry{}, fmt.Errorf("OpenAI tool result capability does not match pending call")
		}
		wantWire := tool.WireStructured
		if calls[index].Type == "custom_tool_call" {
			wantWire = tool.WireCustomFreeform
		}
		if result.WireKind() != wantWire {
			return nativeHistoryEntry{}, fmt.Errorf("OpenAI result wire does not match call")
		}
		payload, err := codec.MarshalStable(functionOutputPayload{
			Status: result.Status(), Code: result.Code(), Content: result.Preview().Text(),
		})
		if err != nil {
			return nativeHistoryEntry{}, fmt.Errorf("encode OpenAI tool result: %w", err)
		}
		outputType := "function_call_output"
		if calls[index].Type == "custom_tool_call" {
			outputType = "custom_tool_call_output"
		}
		outputs[index] = NativeItem{Type: outputType, CallID: calls[index].CallID, Output: string(payload)}
	}
	entry := nativeHistoryEntry{Kind: nativeHistoryToolOutputs, TurnID: history[len(history)-1].TurnID, ToolOutputs: outputs}
	if err := validateNativeHistoryEntry(entry); err != nil {
		return nativeHistoryEntry{}, err
	}
	return entry, nil
}

func openAICapabilityForName(name string) tool.CapabilityID {
	switch name {
	case "exec_command":
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
	case "apply_patch":
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
			pending = functionCalls(entry.Outputs)
		case nativeHistoryToolOutputs:
			if len(pending) == 0 || entry.TurnID != turnID {
				return fmt.Errorf("tool outputs have no matching pending calls")
			}
			if err := matchFunctionOutputs(pending, entry.ToolOutputs); err != nil {
				return err
			}
			pending = nil
			sampleAllowed = true
			steeringAllowed = true
		}
	}
	return nil
}

func pendingFunctionCalls(history []nativeHistoryEntry) ([]NativeItem, error) {
	if err := validateNativeHistory(history); err != nil {
		return nil, err
	}
	if len(history) == 0 || history[len(history)-1].Kind != nativeHistorySample {
		return nil, fmt.Errorf("OpenAI native history has no pending tool calls")
	}
	calls := functionCalls(history[len(history)-1].Outputs)
	if len(calls) == 0 {
		return nil, fmt.Errorf("OpenAI native history has no pending tool calls")
	}
	return calls, nil
}

func functionCalls(items []NativeItem) []NativeItem {
	calls := make([]NativeItem, 0)
	for _, item := range items {
		if item.Type == "function_call" || item.Type == "custom_tool_call" {
			calls = append(calls, item.clone())
		}
	}
	return calls
}

func matchFunctionOutputs(calls []NativeItem, outputs []NativeItem) error {
	if len(calls) != len(outputs) {
		return fmt.Errorf("OpenAI function output count does not match pending calls")
	}
	seen := make(map[string]struct{}, len(outputs))
	for index, output := range outputs {
		wantType := "function_call_output"
		if calls[index].Type == "custom_tool_call" {
			wantType = "custom_tool_call_output"
		}
		if output.Type != wantType || output.CallID != calls[index].CallID {
			return fmt.Errorf("OpenAI tool output order does not match pending calls")
		}
		if _, exists := seen[output.CallID]; exists {
			return fmt.Errorf("OpenAI function output call ID is duplicated")
		}
		seen[output.CallID] = struct{}{}
	}
	return nil
}
