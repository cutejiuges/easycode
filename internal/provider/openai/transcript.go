package openai

import (
	"easycode/internal/domain"
	"easycode/internal/provider"
)

// ProjectTranscript 保留原生提交及内部顺序，仅返回允许展示的文本与调用锚点。
func (conversation *Conversation) ProjectTranscript() (domain.TranscriptView, error) {
	return projectTranscript(conversation.history.snapshot())
}

func projectTranscript(history []nativeHistoryEntry) (domain.TranscriptView, error) {
	return provider.ProjectNativeTranscript(history, domain.ProviderOpenAI, nativeUsers,
		func(input NativeItem) string { return input.Content[0].Text }, appendSampleTranscript, entryStopFeedback)
}

func appendSampleTranscript(items []domain.TranscriptItem, entry nativeHistoryEntry, ordinal uint64) ([]domain.TranscriptItem, error) {
	for index, item := range entry.Outputs {
		position := domain.TranscriptPosition{Entry: ordinal, Item: uint64(index)}
		var v domain.TranscriptItem
		var err error
		switch item.Type {
		case "reasoning":
			body, bodyErr := reasoningBody(item)
			if bodyErr != nil {
				return nil, bodyErr
			}
			v, err = domain.NewTranscriptReasoning(position, entry.TurnID, body)
		case "message":
			if item.Role != "assistant" {
				continue
			}
			v, err = domain.NewTranscriptAssistant(position, entry.TurnID, projectItemText(item, "assistant", "output_text"))
		case "function_call", "custom_tool_call":
			v, err = domain.NewTranscriptCall(position, entry.TurnID, item.CallID, item.Name)
		default:
			continue
		}
		if err != nil {
			return nil, err
		}
		if v.Kind == domain.TranscriptAssistant && v.Text == "" {
			continue
		}
		items = append(items, v)
	}
	return items, nil
}
