package anthropic

import (
	"strings"

	"easycode/internal/domain"
	"easycode/internal/provider"
)

// ProjectHistory 返回已提交 Anthropic 原生历史的独立文本语义快照。
func (conversation *Conversation) ProjectHistory() domain.SemanticHistoryView {
	return projectHistory(conversation.history.snapshot())
}

func projectHistory(history []nativeHistoryEntry) domain.SemanticHistoryView {
	return provider.ProjectNativeHistory(history, domain.ProviderAnthropic, nativeUsers, projectUserText, nativeAssistantText, entryStopFeedback)
}

func nativeUsers(entry nativeHistoryEntry) []nativeMessage { return entry.UserInputs }

func nativeAssistantText(entry nativeHistoryEntry) string {
	return projectMessageText(entry.Assistant, roleAssistant)
}

func projectUserText(input nativeMessage, withAttachments bool) string {
	if !withAttachments {
		return projectMessageText(input, roleUser)
	}
	if input.Role == roleUser && len(input.Content) > 0 && input.Content[0].Type == "text" {
		return input.Content[0].Text
	}
	return ""
}

func projectMessageText(message nativeMessage, role string) string {
	if message.Role != role {
		return ""
	}
	var text strings.Builder
	for _, block := range message.Content {
		if block.Type == blockTypeText {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}
