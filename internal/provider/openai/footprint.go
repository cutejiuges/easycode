package openai

import (
	"easycode/internal/codec"
	"easycode/internal/context/estimate"
	"easycode/internal/domain"
)

// HistoryFootprint 返回下一次 Responses 请求会重放的已提交原生历史估算。
func (conversation *Conversation) HistoryFootprint() (domain.NativeHistoryFootprint, error) {
	turns, revision := conversation.history.snapshotWithRevision()
	var bytes uint64
	for _, turn := range turns {
		user, err := codec.MarshalStable(turn.User)
		if err != nil {
			return unknownHistoryFootprint(revision)
		}
		bytes = estimate.SaturatingAdd(bytes, uint64(len(user)))
		for _, item := range turn.Outputs {
			encoded, err := codec.MarshalStable(item)
			if err != nil {
				return unknownHistoryFootprint(revision)
			}
			bytes = estimate.SaturatingAdd(bytes, uint64(len(encoded)))
		}
	}
	tokens, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristicV1, estimate.ByteCount(bytes))
	return domain.NewNativeHistoryFootprint(domain.ProviderOpenAI, revision, tokens)
}

func unknownHistoryFootprint(revision uint64) (domain.NativeHistoryFootprint, error) {
	tokens, _ := domain.NewUnknownTokenEstimate(estimate.MethodByteHeuristicV1)
	return domain.NewNativeHistoryFootprint(domain.ProviderOpenAI, revision, tokens)
}
