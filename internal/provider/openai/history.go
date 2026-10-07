package openai

import (
	"strings"

	"easycode/internal/domain"
)

// ProjectHistory 返回已提交 OpenAI 原生历史的独立文本语义快照。
func (conversation *Conversation) ProjectHistory() domain.SemanticHistoryView {
	return projectHistory(conversation.history.snapshot())
}

func projectHistory(history []nativeHistoryEntry) domain.SemanticHistoryView {
	turns := make([]domain.SemanticTurn, 0, len(history))
	for _, entry := range history {
		if entry.Kind != nativeHistorySample {
			continue
		}
		assistantText := projectAssistantText(entry.Outputs)
		if entry.Input != nil {
			turns = append(turns, domain.SemanticTurn{
				UserText: projectItemText(*entry.Input, "user", "input_text"), AssistantText: assistantText,
			})
			continue
		}
		if len(turns) > 0 {
			turns[len(turns)-1].AssistantText += assistantText
		}
	}
	return domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: turns}
}

func projectAssistantText(outputs []NativeItem) string {
	var text strings.Builder
	for _, item := range outputs {
		text.WriteString(projectItemText(item, "assistant", "output_text"))
	}
	return text.String()
}

func projectItemText(item NativeItem, role, partType string) string {
	if item.Type != "message" || item.Role != role {
		return ""
	}
	var text strings.Builder
	for _, part := range item.Content {
		if part.Type == partType {
			text.WriteString(part.Text)
		}
	}
	return text.String()
}
