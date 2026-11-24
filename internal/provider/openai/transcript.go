package openai

import (
	"easycode/internal/domain"
	"easycode/internal/provider"
)

// ProjectTranscriptFrom 返回指定 ordinal 起的已提交投影和真实结束位置。
func (conversation *Conversation) ProjectTranscriptFrom(ordinal uint64) (domain.TranscriptView, uint64, error) {
	history, err := conversation.history.EntriesFrom(ordinal)
	if err != nil {
		return domain.TranscriptView{}, 0, err
	}
	view, err := provider.ProjectNativeTranscript(history, ordinal, domain.ProviderOpenAI, nativeUsers,
		func(input NativeItem) string { return input.Content[0].Text }, appendSampleTranscript, entryStopFeedback, entrySessionStartContext)
	return view, ordinal + uint64(len(history)), err
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
