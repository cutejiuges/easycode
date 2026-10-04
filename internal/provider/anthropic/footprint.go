package anthropic

import (
	"easycode/internal/codec"
	"easycode/internal/context/estimate"
	"easycode/internal/domain"
)

// HistoryFootprint 返回下一次 Messages 请求会重放的已提交原生历史估算。
func (conversation *Conversation) HistoryFootprint() (domain.NativeHistoryFootprint, error) {
	turns, revision := conversation.history.snapshotWithRevision()
	var bytes uint64
	for _, turn := range turns {
		user, err := codec.MarshalStable(turn.User)
		if err != nil {
			return unknownHistoryFootprint(revision)
		}
		assistant, err := codec.MarshalStable(turn.Assistant)
		if err != nil {
			return unknownHistoryFootprint(revision)
		}
		bytes = estimate.SaturatingAdd(bytes, uint64(len(user)), uint64(len(assistant)))
	}
	tokens, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristicV1, estimate.ByteCount(bytes))
	return domain.NewNativeHistoryFootprint(domain.ProviderAnthropic, revision, tokens)
}

func unknownHistoryFootprint(revision uint64) (domain.NativeHistoryFootprint, error) {
	tokens, _ := domain.NewUnknownTokenEstimate(estimate.MethodByteHeuristicV1)
	return domain.NewNativeHistoryFootprint(domain.ProviderAnthropic, revision, tokens)
}
