package anthropic

import (
	"bytes"
	"context"
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

// HistoryReader 只验证和投影存储历史，不持有连接或凭据。
type HistoryReader struct{}

// NewHistoryReader 创建纯内存的离线读取策略。
func NewHistoryReader() *HistoryReader { return &HistoryReader{} }

// ProjectStoredHistory 严格校验原生历史后返回独立语义快照。
func (*HistoryReader) ProjectStoredHistory(ctx context.Context, source provider.HistorySource, records []provider.RestoreRecord) (domain.SemanticHistoryView, error) {
	if err := source.Validate(); err != nil {
		return domain.SemanticHistoryView{}, err
	}
	if source.Family != domain.ProviderAnthropic || source.Wire != messagesWire {
		return domain.SemanticHistoryView{}, fmt.Errorf("history source is incompatible")
	}
	if err := ctx.Err(); err != nil {
		return domain.SemanticHistoryView{}, err
	}
	history := &nativeHistory{}
	if err := restoreNativeHistory(ctx, source.Model, records, history); err != nil {
		return domain.SemanticHistoryView{}, err
	}
	view := projectHistory(history.entries)
	if err := ctx.Err(); err != nil {
		return domain.SemanticHistoryView{}, err
	}
	return view, nil
}

// ValidateCompactionUsage 校验失败压缩的用量证据。
func (*HistoryReader) ValidateCompactionUsage(envelope provider.NativeCommitEnvelope, expected domain.SampleUsage) error {
	return validateCompactionUsage(envelope, expected)
}

// ValidateCheckpointUsage 校验已完成压缩的用量证据。
func (*HistoryReader) ValidateCheckpointUsage(checkpoint provider.CheckpointEnvelope, expected domain.SampleUsage) error {
	return validateCheckpointUsage(checkpoint, expected)
}

// restoreNativeHistory 是联网恢复和离线投影共用的事务式验证步骤。
func restoreNativeHistory(ctx context.Context, model string, commits []provider.RestoreRecord, history *nativeHistory) error {
	entries := make([]nativeHistoryEntry, 0, len(commits))
	var questionResults []tool.InvocationResult
	seenCheckpoints := make(map[domain.CompactionID]struct{})
	for index, commit := range commits {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := commit.Validate(); err != nil {
			return err
		}
		if expected, ok := commit.CompactionSource(); ok {
			history.entries = entries
			history.revision = uint64(len(entries))
			source, err := nativeCompactionSource(history.snapshotWindow(), model)
			if err != nil || source != expected {
				return fmt.Errorf("compaction source does not match native history")
			}
			continue
		}

		if commit.IsCheckpoint() {
			checkpoint, err := commit.Checkpoint()
			if err != nil {
				return err
			}
			if _, exists := seenCheckpoints[checkpoint.ID()]; exists {
				return fmt.Errorf("checkpoint identity is duplicated")
			}
			seenCheckpoints[checkpoint.ID()] = struct{}{}
			history.entries = entries
			history.revision = uint64(len(entries))
			source, err := nativeCompactionSource(history.snapshotWindow(), model)
			if err != nil || source != checkpoint.Source() {
				return fmt.Errorf("checkpoint source does not match native history")
			}
			summary, summaryText, err := decodeCheckpoint(checkpoint)
			if err != nil {
				return err
			}
			history.installCheckpoint(checkpoint, summary, summaryText)
			continue
		}

		envelope, err := commit.Envelope()
		if err != nil {
			return err
		}
		entry, err := decodeNativeCommit(envelope)
		if err != nil {
			return fmt.Errorf("restore Anthropic native commit %d: %w", index+1, err)
		}
		if string(entry.Kind) != string(commit.Role()) {
			return fmt.Errorf("native commit role mismatch")
		}
		if err := validateQuestionRestoreFacts(entry, commit.QuestionInvocations()); err != nil {
			return err
		}
		if entry.Kind == nativeHistorySample {
			questionResults = commit.QuestionResults()
		}
		if entry.Kind == nativeHistoryToolOutputs {
			expected, err := prepareToolOutputsEntry(entries, commit.ToolOutputs())
			if err != nil {
				return err
			}
			expectedEnvelope, err := encodeNativeCommit(expected)
			if err != nil {
				return err
			}
			actualEnvelope, err := encodeNativeCommit(entry)
			if err != nil {
				return err
			}
			if !bytes.Equal(expectedEnvelope.Payload(), actualEnvelope.Payload()) {
				return fmt.Errorf("native tool output differs from committed evidence")
			}
			if len(entries) == 0 {
				return fmt.Errorf("tool outputs have no sample")
			}
			if err := validateQuestionRestoredOutputs(entries[len(entries)-1], entry, questionResults); err != nil {
				return err
			}
			questionResults = nil
		}
		if entry.Kind == nativeHistoryHookFeedback {
			expected, ok := commit.StopFeedback()
			actual, err := entryStopFeedback(entry)
			if !ok || err != nil || actual != expected {
				return fmt.Errorf("native hook feedback differs from committed evidence")
			}
		}
		entry.TurnID = commit.TurnID()
		if entry.Kind == nativeHistoryUserInputs {
			a, ok := commit.Association()
			if !ok || entry.InputMetadata == nil || !entry.InputMetadata.Matches(a) {
				return fmt.Errorf("native input association mismatch")
			}
		}
		entries = append(entries, entry.clone())
	}
	if err := validateNativeHistory(entries); err != nil {
		return fmt.Errorf("restore Anthropic native history: %w", err)
	}
	history.entries = entries
	history.revision = uint64(len(entries))
	return ctx.Err()
}
