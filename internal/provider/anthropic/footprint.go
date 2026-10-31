package anthropic

import (
	"easycode/internal/codec"
	"easycode/internal/context/estimate"
	"easycode/internal/domain"
)

// HistoryFootprint 返回下一次 Messages 请求会重放的已提交原生历史估算。
func (conversation *Conversation) HistoryFootprint() (domain.NativeHistoryFootprint, error) {
	snapshot := conversation.history.snapshotWindow()
	return historyFootprint(snapshot.requestEntries(), snapshot.revision+snapshot.count)
}

func historyFootprint(entries []nativeHistoryEntry, revision uint64) (domain.NativeHistoryFootprint, error) {
	var bytes uint64
	for _, entry := range entries {
		for _, userInput := range entry.UserInputs {
			input, err := codec.MarshalStable(userInput)
			if err != nil {
				return unknownHistoryFootprint(revision)
			}
			bytes = estimate.SaturatingAdd(bytes, uint64(len(input)))
		}
		if entry.Kind == nativeHistoryUserInputs {
			continue
		}
		var message nativeMessage
		if entry.Kind == nativeHistorySample {
			message = entry.Assistant
		} else {
			message = entry.ToolOutputs
		}
		encoded, err := codec.MarshalStable(message)
		if err != nil {
			return unknownHistoryFootprint(revision)
		}
		bytes = estimate.SaturatingAdd(bytes, uint64(len(encoded)))
	}
	tokens, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, estimate.ByteCount(bytes))
	return domain.NewNativeHistoryFootprint(domain.ProviderAnthropic, revision, tokens)
}

func unknownHistoryFootprint(revision uint64) (domain.NativeHistoryFootprint, error) {
	tokens, _ := domain.NewUnknownTokenEstimate(estimate.MethodByteHeuristic)
	return domain.NewNativeHistoryFootprint(domain.ProviderAnthropic, revision, tokens)
}
