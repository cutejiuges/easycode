package openai

import (
	"easycode/internal/tool"
	"fmt"
)

// validateQuestionRestoreFacts 在安装历史前核对原生题目与共享 ready，不从展示值反向构造请求。
func validateQuestionRestoreFacts(entry nativeHistoryEntry, questions []tool.QuestionInvocation) error {
	index := 0
	for _, item := range entry.Outputs {
		if item.Type != "function_call" || item.Name != "request_user_input" {
			continue
		}
		if index >= len(questions) {
			return fmt.Errorf("native question has no matching ready fact")
		}
		input, err := tool.DecodeQuestionInput([]byte(item.Arguments))
		if err != nil || string(questions[index].ProviderCallID()) != item.CallID || questions[index].Input() != input {
			return fmt.Errorf("native question does not match ready fact")
		}
		index++
	}
	if index != len(questions) {
		return fmt.Errorf("question ready fact has no native call")
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
				expected, err := encodeToolResult(item, r)
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
