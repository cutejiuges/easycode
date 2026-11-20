package openai

import (
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

func nativeCallEvidence(call NativeItem) provider.NativeToolCall {
	evidence := structuredCallEvidence(call)
	if call.Type == "custom_tool_call" {
		evidence.Wire, evidence.Arguments = tool.WireCustomFreeform, []byte(call.Input)
	}
	return evidence
}

func structuredCallEvidence(call NativeItem) provider.NativeToolCall {
	return provider.NativeToolCall{Name: call.Name, ID: tool.ProviderCallID(call.CallID), Wire: tool.WireStructured, Arguments: []byte(call.Arguments)}
}

func validateNativeReadyFacts(entry nativeHistoryEntry, ready []tool.Invocation) error {
	return provider.ValidateNativeReadyFacts(functionCalls(entry.Outputs), ready, domain.ProviderOpenAI, nativeCallEvidence)
}

func validateQuestionRestoredOutputs(sample, entry nativeHistoryEntry, results []tool.InvocationResult) error {
	return provider.ValidateNativeRestoredResult(functionCalls(sample.Outputs), entry.ToolOutputs, results, "request_user_input", "question", nativeCallEvidence, matchFrozenToolOutput)
}

func validateTaskRestoredOutputs(sample, entry nativeHistoryEntry, results []tool.InvocationResult) error {
	return provider.ValidateNativeRestoredResult(functionCalls(sample.Outputs), entry.ToolOutputs, results, "update_plan", "task", nativeCallEvidence, matchFrozenToolOutput)
}

func matchFrozenToolOutput(call, actual NativeItem, result tool.InvocationResult) bool {
	expected, err := encodeToolResultData(call, result, result.Preview().Text())
	return err == nil && actual.Type == expected.Type && actual.CallID == expected.CallID && actual.Output == expected.Output
}
