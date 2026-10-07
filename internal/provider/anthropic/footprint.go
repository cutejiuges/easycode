package anthropic

import (
	"easycode/internal/codec"
	"easycode/internal/context/estimate"
	"easycode/internal/domain"
)

// HistoryFootprint 返回下一次 Messages 请求会重放的已提交原生历史估算。
func (conversation *Conversation) HistoryFootprint() (domain.NativeHistoryFootprint, error) {
	entries, revision := conversation.history.snapshotWithRevision()
	var bytes uint64
	for _, entry := range entries {
		if entry.Input != nil {
			input, err := codec.MarshalStable(entry.Input)
			if err != nil {
				return unknownHistoryFootprint(revision)
			}
			bytes = estimate.SaturatingAdd(bytes, uint64(len(input)))
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
