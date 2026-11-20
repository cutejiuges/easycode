package anthropic

import (
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

func nativeCallEvidence(call NativeItem) provider.NativeToolCall {
	return provider.NativeToolCall{Name: call.Name, ID: tool.ProviderCallID(call.ID), Wire: tool.WireStructured, Arguments: call.Input}
}

func validateNativeReadyFacts(entry nativeHistoryEntry, ready []tool.Invocation) error {
	return provider.ValidateNativeReadyFacts(toolUses(entry.Assistant.Content), ready, domain.ProviderAnthropic, nativeCallEvidence)
}

func validateQuestionRestoredOutputs(sample, entry nativeHistoryEntry, results []tool.InvocationResult) error {
	return provider.ValidateNativeRestoredResult(toolUses(sample.Assistant.Content), entry.ToolOutputs.Content, results, "AskUserQuestion", "question", nativeCallEvidence, matchFrozenToolOutput)
}

func validateTaskRestoredOutputs(sample, entry nativeHistoryEntry, results []tool.InvocationResult) error {
	return provider.ValidateNativeRestoredResult(toolUses(sample.Assistant.Content), entry.ToolOutputs.Content, results, "TodoWrite", "task", nativeCallEvidence, matchFrozenToolOutput)
}

func matchFrozenToolOutput(call, actual NativeItem, result tool.InvocationResult) bool {
	expected, err := encodeToolResultData(call, result, result.Preview().Text())
	return err == nil && actual.Type == expected.Type && actual.ToolUseID == expected.ToolUseID && actual.Content == expected.Content && actual.IsError == expected.IsError
}
