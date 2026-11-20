package anthropic

import (
	"easycode/internal/domain"
	"easycode/internal/provider"
	"iter"
)

// HistoryFootprint 返回下一次 Messages 请求会重放的已提交原生历史估算。
func (conversation *Conversation) HistoryFootprint() (domain.NativeHistoryFootprint, error) {
	snapshot := conversation.history.snapshotWindow()
	return historyFootprint(snapshot.RequestEntries(), snapshot.Revision+snapshot.Count)
}

func historyFootprint(entries []nativeHistoryEntry, revision uint64) (domain.NativeHistoryFootprint, error) {
	return provider.EstimateNativeHistory(entries, revision, domain.ProviderAnthropic, measureNativeEntry)
}

func measureNativeEntry(entry nativeHistoryEntry) iter.Seq[nativeMessage] {
	return func(yield func(nativeMessage) bool) {
		if entry.FeedbackInput != nil && !yield(*entry.FeedbackInput) {
			return
		}
		for _, input := range entry.UserInputs {
			if !yield(input) {
				return
			}
		}
		switch entry.Kind {
		case nativeHistoryHookFeedback, nativeHistoryUserInputs:
		case nativeHistorySample:
			yield(entry.Assistant)
		default:
			yield(entry.ToolOutputs)
		}
	}
}
