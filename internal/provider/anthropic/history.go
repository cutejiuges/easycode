package anthropic

import (
	"strings"

	"easycode/internal/domain"
)

// ProjectHistory 返回已提交 Anthropic 原生历史的独立文本语义快照。
func (conversation *Conversation) ProjectHistory() domain.SemanticHistoryView {
	return projectHistory(conversation.history.snapshot())
}

func projectHistory(history []nativeTurn) domain.SemanticHistoryView {
	turns := make([]domain.SemanticTurn, 0, len(history))
	for _, turn := range history {
		turns = append(turns, domain.SemanticTurn{
			UserText:      projectMessageText(turn.User, roleUser),
			AssistantText: projectMessageText(turn.Assistant, roleAssistant),
		})
	}
	return domain.SemanticHistoryView{Provider: domain.ProviderAnthropic, Turns: turns}
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
