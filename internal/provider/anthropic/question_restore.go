package anthropic

import (
	"easycode/internal/tool"
	"fmt"
)

// validateQuestionRestoreFacts 在安装历史前核对原生题目与共享 ready，不从展示值反向构造请求。
func validateQuestionRestoreFacts(entry nativeHistoryEntry, questions []tool.QuestionInvocation) error {
	index := 0
	for _, item := range entry.Assistant.Content {
		if item.Type != blockTypeToolUse || item.Name != "AskUserQuestion" {
			continue
		}
		if index >= len(questions) {
			return fmt.Errorf("native question has no matching ready fact")
		}
		input, err := tool.DecodeQuestionInput(item.Input)
		if err != nil || string(questions[index].ProviderCallID()) != item.ID || questions[index].Input() != input {
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
	outputs := entry.ToolOutputs.Content
	for _, item := range sample.Assistant.Content {
		if !(item.Type == blockTypeToolUse) {
			continue
		}
		if item.Name == "AskUserQuestion" {
			if callIndex >= len(outputs) {
				return fmt.Errorf("question output is missing")
			}
			found := false
			for _, r := range results {
				if string(r.ProviderCallID()) != item.ID {
					continue
				}
				expected, err := encodeToolResult(item, r)
				actual := outputs[callIndex]
				if err != nil || actual.Type != expected.Type || actual.ToolUseID != expected.ToolUseID || actual.Content != expected.Content || actual.IsError != expected.IsError {
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
