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
	turns := make([]domain.SemanticTurn, 0)
	for _, entry := range history {
		if entry.Kind == nativeHistoryToolOutputs {
			continue
		}
		if len(turns) == 0 || turns[len(turns)-1].TurnID != entry.TurnID {
			turns = append(turns, domain.SemanticTurn{TurnID: entry.TurnID, Messages: make([]domain.SemanticMessage, 0)})
		}
		turn := &turns[len(turns)-1]
		if entry.Kind == nativeHistoryUserInputs {
			for i, input := range entry.UserInputs {
				turn.Messages = append(turn.Messages, domain.SemanticMessage{Role: domain.SemanticUser, InputID: entry.InputMetadata.InputIDs[i], Text: projectItemText(input, "user", "input_text")})
			}
		} else if text := projectAssistantText(entry.Outputs); text != "" {
			turn.Messages = append(turn.Messages, domain.SemanticMessage{Role: domain.SemanticAssistant, Text: text})
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
