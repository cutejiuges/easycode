package anthropic

import (
	"easycode/internal/domain"
	"easycode/internal/provider"
	"iter"
)

func historyFootprint(entries []nativeHistoryEntry, revision uint64) (domain.NativeHistoryFootprint, error) {
	return provider.EstimateNativeHistory(entries, revision, domain.ProviderAnthropic, nativeRequestMessages)
}

func nativeRequestMessages(entry nativeHistoryEntry) iter.Seq[nativeMessage] {
	return func(yield func(nativeMessage) bool) {
		if entry.SessionStartInput != nil && !yield(*entry.SessionStartInput) {
			return
		}
		if entry.FeedbackInput != nil && !yield(*entry.FeedbackInput) {
			return
		}
		for _, input := range entry.UserInputs {
			if !yield(input) {
				return
			}
		}
		switch entry.Kind {
		case nativeHistorySample:
			yield(entry.Assistant)
		case nativeHistoryToolOutputs:
			yield(entry.ToolOutputs)
		}
	}
}
