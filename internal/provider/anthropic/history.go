package anthropic

import (
	"strings"

	"easycode/internal/domain"
)

// ProjectHistory 返回已提交 Anthropic 原生历史的独立文本语义快照。
func (conversation *Conversation) ProjectHistory() domain.SemanticHistoryView {
	return projectHistory(conversation.history.snapshot())
}

func projectHistory(history []nativeHistoryEntry) domain.SemanticHistoryView {
	turns := make([]domain.SemanticTurn, 0, len(history))
	for _, entry := range history {
		if entry.Kind != nativeHistorySample {
			continue
		}
		assistantText := projectMessageText(entry.Assistant, roleAssistant)
		if entry.Input != nil {
			turns = append(turns, domain.SemanticTurn{
				UserText: projectMessageText(*entry.Input, roleUser), AssistantText: assistantText,
			})
		} else if len(turns) > 0 {
			turns[len(turns)-1].AssistantText += assistantText
		}
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
