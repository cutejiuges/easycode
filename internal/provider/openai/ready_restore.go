package openai

import (
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

func nativeCallEvidence(call NativeItem) provider.NativeToolCall {
	wire, input := nativeToolCallInput(call)
	return provider.NativeToolCall{Name: call.Name, ID: tool.ProviderCallID(call.CallID), Wire: wire, Arguments: []byte(input)}
}

func validateNativeReadyFacts(entry nativeHistoryEntry, ready []tool.Invocation) error {
	return provider.ValidateNativeReadyFacts(functionCalls(entry.Outputs), ready, domain.ProviderOpenAI, nativeCallEvidence)
}

func validateRestoredOutputs(calls []NativeItem, entry nativeHistoryEntry, results []tool.InvocationResult) error {
	return provider.ValidateNativeRestoredResults(calls, entry.ToolOutputs, results, domain.ProviderOpenAI, nativeCallEvidence, matchFrozenToolOutput)
}

func matchFrozenToolOutput(call, actual NativeItem, result tool.InvocationResult) bool {
	expected, err := encodeToolResultData(call, result, result.Preview().Text())
	return err == nil && actual.Type == expected.Type && actual.CallID == expected.CallID && actual.Output == expected.Output
}
