package openai

import (
	"easycode/internal/domain"
	"easycode/internal/tool"
	"fmt"
)

// validateNativeReadyFacts 在安装历史前核对全部原始 ready，不访问执行环境。
func validateNativeReadyFacts(entry nativeHistoryEntry, ready []tool.Invocation) error {
	index := 0
	for _, item := range entry.Outputs {
		if item.Type != "function_call" && item.Type != "custom_tool_call" {
			continue
		}
		if index >= len(ready) {
			return fmt.Errorf("native call has no matching ready fact")
		}
		wire, arguments := tool.WireStructured, []byte(item.Arguments)
		if item.Type == "custom_tool_call" {
			wire, arguments = tool.WireCustomFreeform, []byte(item.Input)
		}
		if err := tool.ValidateNativeCallInput(ready[index], domain.ProviderOpenAI, item.Name, tool.ProviderCallID(item.CallID), wire, arguments); err != nil {
			return err
		}
		index++
	}
	if index != len(ready) {
		return fmt.Errorf("ready fact has no native call")
	}
	return nil
}

// validateQuestionRestoredOutputs 要求原生结果与已提交的冻结答案一致。
func validateQuestionRestoredOutputs(sample, entry nativeHistoryEntry, results []tool.InvocationResult) error {
	callIndex, matched := 0, 0
	outputs := entry.ToolOutputs
	for _, item := range sample.Outputs {
		if !(item.Type == "function_call" || item.Type == "custom_tool_call") {
			continue
		}
		if item.Name == "request_user_input" {
			if callIndex >= len(outputs) {
				return fmt.Errorf("question output is missing")
			}
			found := false
			for _, r := range results {
				if string(r.ProviderCallID()) != item.CallID {
					continue
				}
				expected, err := encodeToolResultData(item, r, r.Preview().Text())
				actual := outputs[callIndex]
				if err != nil || actual.Type != expected.Type || actual.CallID != expected.CallID || actual.Output != expected.Output {
					return fmt.Errorf("native question output differs from committed result")
				}
				found = true
				matched++
				break
			}
			if !found {
				return fmt.Errorf("question output has no committed result")
			}
		}
		callIndex++
	}
	if matched != len(results) {
		return fmt.Errorf("committed question result has no output")
	}
	return nil
}
