package anthropic

import (
	"easycode/internal/domain"
	"easycode/internal/provider"
)

// ProjectTranscript 保留原生提交及内部顺序，仅返回允许展示的文本与调用锚点。
func (conversation *Conversation) ProjectTranscript() (domain.TranscriptView, error) {
	return projectTranscript(conversation.history.snapshot())
}

func projectTranscript(history []nativeHistoryEntry) (domain.TranscriptView, error) {
	return provider.ProjectNativeTranscript(history, domain.ProviderAnthropic, nativeUsers,
		func(input nativeMessage) string { return input.Content[0].Text }, appendSampleTranscript, entryStopFeedback)
}

func appendSampleTranscript(items []domain.TranscriptItem, entry nativeHistoryEntry, ordinal uint64) ([]domain.TranscriptItem, error) {
	for index, item := range entry.Assistant.Content {
		position := domain.TranscriptPosition{Entry: ordinal, Item: uint64(index)}
		var v domain.TranscriptItem
		var err error
		switch item.Type {
		case blockTypeThinking, blockTypeRedactedThinking:
			body, bodyErr := reasoningBody(item)
			if bodyErr != nil {
				return nil, bodyErr
			}
			v, err = domain.NewTranscriptReasoning(position, entry.TurnID, body)
		case blockTypeText:
			v, err = domain.NewTranscriptAssistant(position, entry.TurnID, item.Text)
		case blockTypeToolUse:
			v, err = domain.NewTranscriptCall(position, entry.TurnID, item.ID, item.Name)
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
