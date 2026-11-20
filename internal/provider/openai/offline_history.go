package openai

import (
	"context"

	"easycode/internal/domain"
	"easycode/internal/provider"
)

// HistoryReader 只验证和投影存储历史，不持有连接或凭据。
type HistoryReader struct{}

// NewHistoryReader 创建纯内存的离线读取策略。
func NewHistoryReader() *HistoryReader { return &HistoryReader{} }

// ProjectStoredHistory 严格校验原生历史后返回独立语义快照。
func (*HistoryReader) ProjectStoredHistory(ctx context.Context, source provider.HistorySource, records []provider.RestoreRecord) (domain.SemanticHistoryView, error) {
	return provider.ProjectStoredNativeHistory(ctx, source, domain.ProviderOpenAI, responsesWire, records, nativeSequenceRules(), nativeReplayCodec(), projectHistory)
}

// ValidateCompactionUsage 校验失败压缩的用量证据。
func (*HistoryReader) ValidateCompactionUsage(envelope provider.NativeCommitEnvelope, expected domain.SampleUsage) error {
	return validateCompactionUsage(envelope, expected)
}

// ValidateCheckpointUsage 校验已完成压缩的用量证据。
func (*HistoryReader) ValidateCheckpointUsage(checkpoint provider.CheckpointEnvelope, expected domain.SampleUsage) error {
	return validateCheckpointUsage(checkpoint, expected)
}

func nativeReplayCodec() provider.NativeReplayCodec[nativeHistoryEntry, NativeItem] {
	return provider.NativeReplayCodec[nativeHistoryEntry, NativeItem]{
		Name: "OpenAI", PendingUnavailable: pendingCallsUnavailable, Decode: decodeNativeCommit, Encode: encodeNativeCommit,
		ReadyFacts: validateNativeReadyFacts, ToolOutputs: toolOutputsEntry,
		QuestionResults: validateQuestionRestoredOutputs, TaskResults: validateTaskRestoredOutputs,
		CompactionSource: nativeCompactionSourceChecked, DecodeCheckpoint: decodeCheckpoint, CheckpointTasks: validateCheckpointTaskProgress,
	}
}

// restoreNativeHistory 是联网恢复和离线投影共用的事务式验证步骤。
func restoreNativeHistory(ctx context.Context, model string, commits []provider.RestoreRecord, history *nativeHistory) error {
	return provider.RestoreNativeHistory(ctx, model, commits, &history.NativeHistory, nativeSequenceRules(), nativeReplayCodec())
}
