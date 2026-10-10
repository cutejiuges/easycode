package openai

import (
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
		payload, err := codec.MarshalStable(functionOutputPayload{
			Status: result.Status(), Code: result.Code(), Content: result.Preview().Text(),
		})
		if err != nil {
			return nativeHistoryEntry{}, fmt.Errorf("encode OpenAI tool result: %w", err)
		}
		outputs[index] = NativeItem{
			Type: "function_call_output", CallID: calls[index].CallID, Output: string(payload),
		}
	}
	entry := nativeHistoryEntry{Kind: nativeHistoryToolOutputs, ToolOutputs: outputs}
	if err := validateNativeHistoryEntry(entry); err != nil {
		return nativeHistoryEntry{}, err
	}
	return entry, nil
}

func openAICapabilityForName(name string) tool.CapabilityID {
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
				return fmt.Errorf("OpenAI sample appears before pending function calls are closed")
			}
			if expectContinuation {
				expectContinuation = false
			} else if entry.Input == nil {
				return fmt.Errorf("OpenAI sample input is required at this history position")
			}
			pending = functionCalls(entry.Outputs)
		case nativeHistoryToolOutputs:
			if len(pending) == 0 {
				return fmt.Errorf("OpenAI tool outputs have no pending function calls")
			}
			if err := matchFunctionOutputs(pending, entry.ToolOutputs); err != nil {
				return err
			}
			pending = nil
			expectContinuation = true
		}
	}
	return nil
}

func pendingFunctionCalls(history []nativeHistoryEntry) ([]NativeItem, error) {
	if err := validateNativeHistory(history); err != nil {
		return nil, err
	}
	if len(history) == 0 || history[len(history)-1].Kind != nativeHistorySample {
		return nil, fmt.Errorf("OpenAI native history has no pending function calls")
	}
	calls := functionCalls(history[len(history)-1].Outputs)
	if len(calls) == 0 {
		return nil, fmt.Errorf("OpenAI native history has no pending function calls")
	}
	return calls, nil
}

func functionCalls(items []NativeItem) []NativeItem {
	calls := make([]NativeItem, 0)
	for _, item := range items {
		if item.Type == "function_call" {
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
		if output.Type != "function_call_output" || output.CallID != calls[index].CallID {
			return fmt.Errorf("OpenAI function output order does not match pending calls")
		}
		if _, exists := seen[output.CallID]; exists {
			return fmt.Errorf("OpenAI function output call ID is duplicated")
		}
		seen[output.CallID] = struct{}{}
	}
	return nil
}
