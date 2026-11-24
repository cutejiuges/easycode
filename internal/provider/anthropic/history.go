package anthropic

import (
	"strings"

	"easycode/internal/domain"
	"easycode/internal/provider"
)

var _ provider.PlanProposalProjector = (*Conversation)(nil)

// ProjectPlanProposal 只审阅当前终结采样的助手正文，排除显式截断和工具调用。
func (conversation *Conversation) ProjectPlanProposal(reference domain.AssistantSampleReference) (string, bool) {
	if conversation == nil {
		return "", false
	}
	entry, ok := provider.LatestNativeAssistantSample(&conversation.history, reference)
	if !ok || entry.Metadata.StopReason.Known && entry.Metadata.StopReason.Value != "end_turn" {
		return "", false
	}
	for _, block := range entry.Assistant.Content {
		if block.Type == blockTypeToolUse {
			return "", false
		}
	}
	return domain.ParsePlanProposal(nativeAssistantText(entry))
}

// ProjectHistory 返回已提交 Anthropic 原生历史的独立文本语义快照。
func (conversation *Conversation) ProjectHistory() domain.SemanticHistoryView {
	return projectHistory(conversation.history.Entries())
}

func projectHistory(history []nativeHistoryEntry) domain.SemanticHistoryView {
	return provider.ProjectNativeHistory(history, domain.ProviderAnthropic, nativeUsers, projectUserText, nativeAssistantText, entryStopFeedback, entrySessionStartContext)
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
