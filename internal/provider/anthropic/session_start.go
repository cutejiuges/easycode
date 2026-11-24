package anthropic

import (
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/provider"
)

var _ provider.SessionStartPreparer = (*Conversation)(nil)

// PrepareSessionStartInputs 冻结上下文与真实初始输入的同批原生候选。
func (conversation *Conversation) PrepareSessionStartInputs(value domain.SessionStartContext, association domain.InputAssociation, inputs []domain.UserInput) (*provider.PreparedSessionStartContext, error) {
	return conversation.inputPreparation().PrepareSessionStartInputs(value, association, inputs)
}

// PrepareCompactSessionStartContext 预检已安装 checkpoint 的独立补充。
func (conversation *Conversation) PrepareCompactSessionStartContext(value domain.SessionStartContext, next provider.SessionStartContinuation) (*provider.PreparedSessionStartContext, error) {
	return conversation.inputPreparation().PrepareCompactSessionStartContext(value, next)
}

func sessionStartEntry(value domain.SessionStartContext) (nativeHistoryEntry, error) {
	metadata, err := provider.NewSessionStartMetadata(value)
	if err != nil {
		return nativeHistoryEntry{}, err
	}
	content := make([]NativeItem, len(value.Items))
	for i, item := range value.Items {
		content[i] = NativeItem{Type: blockTypeText, Text: item.Text}
	}
	input := nativeMessage{Role: roleUser, Content: content}
	return nativeHistoryEntry{Kind: nativeHistorySessionStart, TurnID: value.Target.TurnID, SessionStartInput: &input, SessionStartMetadata: &metadata}, nil
}

func entrySessionStartContext(entry nativeHistoryEntry) (domain.SessionStartContext, error) {
	if entry.Kind != nativeHistorySessionStart || entry.SessionStartInput == nil || entry.SessionStartMetadata == nil ||
		entry.TurnID != entry.SessionStartMetadata.Target.TurnID || validateUserTextMessage(*entry.SessionStartInput) != nil {
		return domain.SessionStartContext{}, fmt.Errorf("invalid session start native context")
	}
	texts := make([]string, len(entry.SessionStartInput.Content))
	for i, part := range entry.SessionStartInput.Content {
		texts[i] = part.Text
	}
	return entry.SessionStartMetadata.Context(texts)
}
