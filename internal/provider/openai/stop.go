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
	return provider.ProjectNativeAssistantSample(conversation.history.Entries(), reference, nativeAssistantText)
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
	if conversation == nil {
		return nil, fmt.Errorf("conversation is active")
	}
	return conversation.inputPreparation().PrepareStop(input)
}
