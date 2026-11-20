package openai

import (
	"easycode/internal/domain"
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

type functionOutputPayload struct {
	Status  tool.ResultStatus `json:"status"`
	Code    string            `json:"code"`
	Content string            `json:"content"`
}

func prepareToolOutputsEntry(history []nativeHistoryEntry, results []tool.ModelOutput) (nativeHistoryEntry, error) {
	calls, err := pendingFunctionCalls(history)
	if err != nil {
		return nativeHistoryEntry{}, err
	}
	return toolOutputsEntry(history[len(history)-1].TurnID, calls, results)
}

func toolOutputsEntry(turn domain.TurnID, calls []NativeItem, results []tool.ModelOutput) (nativeHistoryEntry, error) {
	items, err := provider.EncodeNativeToolOutputs(calls, results, "OpenAI tool result count does not match pending calls", encodeToolResult)
	if err != nil {
		return nativeHistoryEntry{}, err
	}
	entry := nativeHistoryEntry{Kind: nativeHistoryToolOutputs, TurnID: turn, ToolOutputs: items}
	if err := validateNativeHistoryEntry(entry); err != nil {
		return nativeHistoryEntry{}, err
	}
	return entry, nil
}

func openAICapabilityForName(name string) tool.CapabilityID {
	capability, _ := tool.FacadeCapability(domain.ProviderOpenAI, name)
	return capability
}

func nativeSequenceRules() provider.NativeSequenceRules[nativeHistoryEntry, NativeItem] {
	return provider.NativeSequenceRules[nativeHistoryEntry, NativeItem]{
		ValidateEntry: validateNativeHistoryEntry,
		Calls:         func(entry nativeHistoryEntry) []NativeItem { return functionCalls(entry.Outputs) },
		MatchOutputs: func(calls []NativeItem, entry nativeHistoryEntry) error {
			return matchFunctionOutputs(calls, entry.ToolOutputs)
		},
		Feedback: entryStopFeedback,
	}
}

func validateNativeHistory(entries []nativeHistoryEntry) error {
	return provider.ValidateNativeSequence(entries, nativeSequenceRules())
}

const pendingCallsUnavailable = "OpenAI native history has no pending tool calls"

func pendingFunctionCalls(history []nativeHistoryEntry) ([]NativeItem, error) {
	return provider.PendingNativeCalls(history, nativeSequenceRules(), pendingCallsUnavailable)
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
		if calls[index].Name == "request_user_input" {
			input, err := tool.DecodeQuestionInput([]byte(calls[index].Arguments))
			if err != nil {
				return err
			}
			var payload functionOutputPayload
			if codec.UnmarshalRecord([]byte(output.Output), &payload) != nil || !payload.Status.Valid() {
				return fmt.Errorf("invalid native question output")
			}
			if payload.Status == tool.ResultSuccess {
				if payload.Code != "ok" {
					return fmt.Errorf("invalid question success code")
				}
				if _, err := tool.DecodeQuestionPreview(input, []byte(payload.Content)); err != nil {
					return err
				}
			}
		}
		seen[output.CallID] = struct{}{}
	}
	return nil
}
