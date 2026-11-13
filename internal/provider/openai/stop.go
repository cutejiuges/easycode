package openai

import (
	"easycode/internal/domain"
	"easycode/internal/provider"
	"fmt"
)

var _ provider.StopContinuationPreparer = (*Conversation)(nil)
var _ provider.StopFeedbackEncoder = (*Conversation)(nil)

// ProjectAssistantSample 按真实采样身份只读提取可见正文，空正文不回退历史。
func (conversation *Conversation) ProjectAssistantSample(reference domain.AssistantSampleReference) (domain.AssistantSampleText, error) {
	if conversation == nil || reference.Validate() != nil {
		return domain.AssistantSampleText{}, fmt.Errorf("invalid assistant sample reference")
	}
	return projectAssistantSample(conversation.history.snapshot(), reference)
}
func projectAssistantSample(history []nativeHistoryEntry, reference domain.AssistantSampleReference) (domain.AssistantSampleText, error) {
	if reference.Validate() != nil {
		return domain.AssistantSampleText{}, fmt.Errorf("invalid assistant sample reference")
	}
	var index uint32
	for _, entry := range history {
		if entry.TurnID != reference.TurnID || entry.Kind != nativeHistorySample {
			continue
		}
		if index == reference.SampleIndex {
			return domain.NewAssistantSampleText(reference, projectAssistantText(entry.Outputs))
		}
		index++
	}
	return domain.AssistantSampleText{}, fmt.Errorf("committed assistant sample was not found")
}

// EncodeStopFeedback 只编码已经冻结的模型字节，不读取磁盘、配置或网络。
func (conversation *Conversation) EncodeStopFeedback(feedback domain.StopFeedback) (provider.NativeCommitEnvelope, error) {
	entry, err := stopFeedbackEntry(feedback)
	if err != nil {
		return provider.NativeCommitEnvelope{}, err
	}
	return encodeNativeCommit(entry)
}
func stopFeedbackEntry(feedback domain.StopFeedback) (nativeHistoryEntry, error) {
	metadata, err := provider.NewHookFeedbackMetadata(feedback)
	if err != nil {
		return nativeHistoryEntry{}, err
	}
	input := NewUserItem(feedback.Text)
	sample, _ := feedback.Identity.Target.AssistantSample()
	entry := nativeHistoryEntry{Kind: nativeHistoryHookFeedback, TurnID: sample.TurnID, FeedbackInput: &input, FeedbackMetadata: &metadata}
	return entry, validateNativeHistoryEntry(entry)
}
func entryStopFeedback(entry nativeHistoryEntry) (domain.StopFeedback, error) {
	if entry.Kind != nativeHistoryHookFeedback || entry.FeedbackInput == nil || entry.FeedbackMetadata == nil {
		return domain.StopFeedback{}, fmt.Errorf("invalid hook feedback entry")
	}
	input := entry.FeedbackInput
	if len(input.Content) != 1 || validateUserItem(*input) != nil {
		return domain.StopFeedback{}, fmt.Errorf("invalid hook feedback message")
	}
	return entry.FeedbackMetadata.Feedback(input.Content[0].Text)
}

// PrepareStopContinuation 以同一候选快照和一个清理 owner 准备反馈及可选用户纠正。
func (conversation *Conversation) PrepareStopContinuation(input domain.StopContinuationInput) (*provider.PreparedStopContinuation, error) {
	if err := input.Validate(); err != nil {
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
	history, revision := conversation.history.snapshotWithRevision()
	feedback, err := stopFeedbackEntry(input.Feedback())
	if err != nil {
		return nil, err
	}
	entries := []nativeHistoryEntry{feedback}
	feedbackEnvelope, err := encodeNativeCommit(feedback)
	if err != nil {
		return nil, err
	}
	var userEnvelope provider.NativeCommitEnvelope
	if association, ok := input.Association(); ok {
		user, err := userInputEntry(association, input.Inputs())
		if err != nil {
			return nil, err
		}
		userEnvelope, err = encodeNativeCommit(user)
		if err != nil {
			return nil, err
		}
		entries = append(entries, user)
	}
	if revision > ^uint64(0)-uint64(len(entries)) {
		return nil, fmt.Errorf("native history revision overflow")
	}
	candidate := append(history, entries...)
	if err := validateNativeHistory(candidate); err != nil {
		return nil, err
	}
	nextRevision := revision + uint64(len(entries))
	footprint, err := conversation.windowFootprint(candidate, nextRevision)
	if err != nil {
		return nil, err
	}
	activeView, err := conversation.projectActiveWindow(candidate, nextRevision)
	if err != nil {
		return nil, err
	}
	preview, err := provider.NewInputPreview(activeView, footprint, func(next provider.TurnInput) error {
		_, err := conversation.compileSamplingRequest(candidate, next)
		return err
	})
	if err != nil {
		return nil, err
	}
	finalize := func() { conversation.history.commitBatch(entries); conversation.pending.Store(false) }
	discard := func() { conversation.pending.Store(false) }
	var prepared *provider.PreparedStopContinuation
	if input.Kind() == domain.StopWithSteering {
		prepared, err = provider.NewPreparedSteeredStopContinuation(input, feedbackEnvelope, userEnvelope, finalize, discard, preview)
	} else {
		prepared, err = provider.NewPreparedStopContinuation(input, feedbackEnvelope, finalize, discard, preview)
	}
	if err != nil {
		return nil, err
	}
	success = true
	return prepared, nil
}

// commitBatch 在一个有界内存转换中发布实际一/两个 entries，revision 不按对象数量推测。
func (history *nativeHistory) commitBatch(entries []nativeHistoryEntry) {
	cloned := make([]nativeHistoryEntry, len(entries))
	for i, entry := range entries {
		cloned[i] = entry.clone()
	}
	history.mu.Lock()
	history.entries = append(history.entries, cloned...)
	history.revision += uint64(len(cloned))
	history.mu.Unlock()
}
