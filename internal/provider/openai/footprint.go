package openai

import (
	"easycode/internal/domain"
	"easycode/internal/provider"
	"iter"
)

// HistoryFootprint 返回下一次 Responses 请求会重放的已提交原生历史估算。
func (conversation *Conversation) HistoryFootprint() (domain.NativeHistoryFootprint, error) {
	snapshot := conversation.history.snapshotWindow()
	return historyFootprint(snapshot.RequestEntries(), snapshot.Revision+snapshot.Count)
}

func historyFootprint(entries []nativeHistoryEntry, revision uint64) (domain.NativeHistoryFootprint, error) {
	return provider.EstimateNativeHistory(entries, revision, domain.ProviderOpenAI, measureNativeEntry)
}

func measureNativeEntry(entry nativeHistoryEntry) iter.Seq[NativeItem] {
	return func(yield func(NativeItem) bool) {
		if entry.FeedbackInput != nil && !yield(*entry.FeedbackInput) {
			return
		}
		for _, input := range entry.UserInputs {
			if !yield(input) {
				return
			}
		}
		for _, item := range entry.Outputs {
			if !yield(item) {
				return
			}
		}
		for _, item := range entry.ToolOutputs {
			if !yield(item) {
				return
			}
		}
	}
}
