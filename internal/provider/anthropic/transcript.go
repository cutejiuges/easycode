package anthropic

import (
	"easycode/internal/domain"
	"errors"
)

// ProjectTranscript 保留原生提交及内部顺序，仅返回允许展示的文本与调用锚点。
func (conversation *Conversation) ProjectTranscript() (domain.TranscriptView, error) {
	return projectTranscript(conversation.history.snapshot())
}

func projectTranscript(history []nativeHistoryEntry) (domain.TranscriptView, error) {
	items := make([]domain.TranscriptItem, 0)
	for ordinal, entry := range history {
		if entry.Kind == nativeHistoryToolOutputs {
			continue
		}
		if entry.Kind == nativeHistoryUserInputs {
			if entry.InputMetadata == nil || len(entry.InputMetadata.InputIDs) != len(entry.UserInputs) {
				return domain.TranscriptView{}, errors.New("invalid transcript input association")
			}
			for index, input := range entry.UserInputs {
				v, err := domain.NewTranscriptUser(domain.TranscriptPosition{Entry: uint64(ordinal), Item: uint64(index)}, entry.TurnID, entry.InputMetadata.InputIDs[index], projectMessageText(input, roleUser))
				if err != nil {
					return domain.TranscriptView{}, err
				}
				items = append(items, v)
			}
			continue
		}
		if entry.Kind != nativeHistorySample {
			return domain.TranscriptView{}, errors.New("invalid transcript entry")
		}
		for index, item := range entry.Assistant.Content {
			position := domain.TranscriptPosition{Entry: uint64(ordinal), Item: uint64(index)}
			var v domain.TranscriptItem
			var err error
			switch item.Type {
			case blockTypeText:
				v, err = domain.NewTranscriptAssistant(position, entry.TurnID, item.Text)
			case blockTypeToolUse:
				v, err = domain.NewTranscriptCall(position, entry.TurnID, item.ID, item.Name)
			default:
				continue
			}
			if err != nil {
				return domain.TranscriptView{}, err
			}
			if v.Kind == domain.TranscriptAssistant && v.Text == "" {
				continue
			}
			items = append(items, v)
		}
	}
	return domain.NewTranscriptView(domain.ProviderAnthropic, items)
}
