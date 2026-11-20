package openai

import (
	"easycode/internal/domain"
	"easycode/internal/provider"
	"iter"
)

func historyFootprint(entries []nativeHistoryEntry, revision uint64) (domain.NativeHistoryFootprint, error) {
	return provider.EstimateNativeHistory(entries, revision, domain.ProviderOpenAI, nativeRequestItems)
}

func nativeRequestItems(entry nativeHistoryEntry) iter.Seq[NativeItem] {
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
