package openai

import (
	"strings"

	"easycode/internal/domain"
	"easycode/internal/provider"
)

var _ provider.PlanProposalProjector = (*Conversation)(nil)

// ProjectPlanProposal 供活动回合收口后从已提交采样选取一条最终助手消息。
func (conversation *Conversation) ProjectPlanProposal(reference domain.AssistantSampleReference) (string, bool) {
	if conversation == nil {
		return "", false
	}
	reviewedTurn := conversation.planReviewTurn.Load()
	if reviewedTurn == nil || *reviewedTurn != reference.TurnID {
		return "", false
	}
	entry, ok := provider.LatestNativeAssistantSample(&conversation.history, reference)
	if !ok || conversation.planReviewTurn.Load() != reviewedTurn {
		return "", false
	}
	var final, unphased string
	visible := 0
	for _, item := range entry.Outputs {
		if item.Type == "function_call" || item.Type == "custom_tool_call" {
			return "", false
		}
		text := projectItemText(item, "assistant", "output_text")
		if strings.TrimSpace(text) == "" {
			continue
		}
		visible++
		switch item.Phase {
		case "final_answer":
			if final != "" {
				return "", false
			}
			final = text
		case "":
			unphased = text
		}
	}
	if final == "" && visible == 1 {
		final = unphased
	}
	return domain.ParsePlanProposal(final)
}

// ProjectHistory 返回已提交 OpenAI 原生历史的独立文本语义快照。
func (conversation *Conversation) ProjectHistory() domain.SemanticHistoryView {
	return projectHistory(conversation.history.Entries())
}

func projectHistory(history []nativeHistoryEntry) domain.SemanticHistoryView {
	return provider.ProjectNativeHistory(history, domain.ProviderOpenAI, nativeUsers, projectUserText, nativeAssistantText, entryStopFeedback)
}

func nativeUsers(entry nativeHistoryEntry) []NativeItem { return entry.UserInputs }

func nativeAssistantText(entry nativeHistoryEntry) string { return projectAssistantText(entry.Outputs) }

func projectUserText(input NativeItem, withAttachments bool) string {
	if !withAttachments {
		return projectItemText(input, "user", "input_text")
	}
	if input.Type == "message" && input.Role == "user" && len(input.Content) > 0 && input.Content[0].Type == "input_text" {
		return input.Content[0].Text
	}
	return ""
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
