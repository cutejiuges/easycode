package anthropic

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"fmt"
)

var _ provider.UserInputPreparer = (*Conversation)(nil)

// PrepareUserInputs 纯内存冻结有序输入及同一版本历史的预检快照。
func (conversation *Conversation) PrepareUserInputs(a domain.InputAssociation, inputs []domain.UserInput) (*provider.PreparedUserInputs, error) {
	if err := provider.ValidateUserInputs(a, inputs); err != nil {
		return nil, err
	}
	if conversation == nil || conversation.active.Load() {
		return nil, fmt.Errorf("conversation is active")
	}
	if !conversation.pending.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("conversation has an unresolved commit")
	}
	success := false
	defer func() {
		if !success {
			conversation.pending.Store(false)
		}
	}()
	entry, err := userInputEntry(a, inputs)
	if err != nil {
		return nil, err
	}
	history, revision := conversation.history.snapshotWithRevision()
	candidate := append(history, entry.clone())
	if err := validateNativeHistory(candidate); err != nil {
		return nil, err
	}
	envelope, err := encodeNativeCommit(entry)
	if err != nil {
		return nil, err
	}
	footprint, err := conversation.windowFootprint(candidate, revision+1)
	if err != nil {
		return nil, err
	}
	activeView, err := conversation.projectActiveWindow(candidate, revision+1)
	if err != nil {
		return nil, err
	}
	preview, err := provider.NewInputPreview(activeView, footprint, func(input provider.TurnInput) error {
		_, err := conversation.compileSamplingRequest(candidate, input)
		return err
	})
	if err != nil {
		return nil, err
	}
	prepared, err := provider.NewPreparedUserInputs(a, envelope, func() { conversation.history.commit(entry); conversation.pending.Store(false) }, func() { conversation.pending.Store(false) }, preview)
	if err != nil {
		return nil, err
	}
	success = true
	return prepared, nil
}

// compileSamplingRequest 是预检与实际采样共用的纯内存编译入口。
func (conversation *Conversation) compileSamplingRequest(history []nativeHistoryEntry, input provider.TurnInput) (codec.CanonicalJSON, error) {
	history = conversation.windowRequestEntries(history)
	snapshot, exists, err := input.ProjectInstructions()
	if err != nil {
		return codec.CanonicalJSON{}, err
	}
	var instructions *domain.ProjectInstructionsSnapshot
	if exists {
		instructions = &snapshot
	}
	catalog, exists, err := input.ToolCatalog()
	if err != nil || !exists {
		return codec.CanonicalJSON{}, fmt.Errorf("tool catalog is required")
	}
	view, err := catalog.View(domain.ProviderAnthropic)
	if err != nil {
		return codec.CanonicalJSON{}, err
	}
	return compileMessagesRequest(conversation.provider.config.Model, conversation.provider.config.MaxOutputTokens, history, instructions, view)
}

// EncodeUserInputs 不读取会话状态，仅验证用户消息的可表示性。
func (conversation *Conversation) EncodeUserInputs(a domain.InputAssociation, inputs []domain.UserInput) (provider.NativeCommitEnvelope, error) {
	entry, err := userInputEntry(a, inputs)
	if err != nil {
		return provider.NativeCommitEnvelope{}, err
	}
	return encodeNativeCommit(entry)
}

func userInputEntry(a domain.InputAssociation, inputs []domain.UserInput) (nativeHistoryEntry, error) {
	if err := provider.ValidateUserInputs(a, inputs); err != nil {
		return nativeHistoryEntry{}, err
	}
	metadata, err := provider.NewInputMetadata(a)
	if err != nil {
		return nativeHistoryEntry{}, err
	}
	entry := nativeHistoryEntry{Kind: nativeHistoryUserInputs, TurnID: a.TurnID(), InputMetadata: &metadata, UserInputs: make([]nativeMessage, len(inputs))}
	for i, input := range inputs {
		entry.UserInputs[i] = newUserMessage(input.Text())
	}
	return entry, nil
}
