package provider

import (
	"bytes"
	"context"
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

type nativeReplayEntry[E any] interface {
	nativeClone[E]
	nativeSequenceEntry
	WithNativeTurn(domain.TurnID) E
}

// NativeReplayCodec 提供回放所需的原生编解码及独立证据验证。
type NativeReplayCodec[E nativeReplayEntry[E], C any] struct {
	Name               string
	PendingUnavailable string
	Decode             func(NativeCommitEnvelope) (E, error)
	Encode             func(E) (NativeCommitEnvelope, error)
	ReadyFacts         func(E, []tool.Invocation) error
	ToolOutputs        func(domain.TurnID, []C, []tool.ModelOutput) (E, error)
	QuestionResults    func(E, E, []tool.InvocationResult) error
	TaskResults        func(E, E, []tool.InvocationResult) error
	CompactionSource   func(NativeWindow[E], string, func() error) (CompactionSource, error)
	DecodeCheckpoint   func(CheckpointEnvelope) (E, domain.ContextSummary, error)
	CheckpointTasks    func(CheckpointEnvelope, domain.TaskProgressState) error
}

type nativeReplay[E nativeReplayEntry[E], C any] struct {
	history         *NativeHistory[E]
	sequence        *nativeSequence[E, C]
	codec           NativeReplayCodec[E, C]
	questionResults []tool.InvocationResult
	taskResults     []tool.InvocationResult
}

// RestoreNativeHistory 回放尚未发布的新历史。各检查点保留既有的错误优先顺序。
func RestoreNativeHistory[E nativeReplayEntry[E], C any](ctx context.Context, model string, records []RestoreRecord, history *NativeHistory[E], rules NativeSequenceRules[E, C], codec NativeReplayCodec[E, C]) error {
	history.entries = make([]E, 0, len(records))
	replay := nativeReplay[E, C]{history: history, sequence: newNativeSequence(rules), codec: codec}
	seenCheckpoints := make(map[domain.CompactionID]struct{})
	for index, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := record.Validate(); err != nil {
			return err
		}
		if expected, ok := record.CompactionSource(); ok {
			if source, err := replay.compactionSource(model); err != nil || source != expected {
				return fmt.Errorf("compaction source does not match native history")
			}
			continue
		}
		if record.IsCheckpoint() {
			checkpoint, err := record.Checkpoint()
			if err != nil {
				return err
			}
			if _, exists := seenCheckpoints[checkpoint.ID()]; exists {
				return fmt.Errorf("checkpoint identity is duplicated")
			}
			seenCheckpoints[checkpoint.ID()] = struct{}{}
			if err := replay.checkpoint(model, record, checkpoint); err != nil {
				return err
			}
			continue
		}
		if err := replay.commit(index, record); err != nil {
			return err
		}
	}
	if replay.sequence.err != nil {
		return fmt.Errorf("restore %s native history: %w", codec.Name, replay.sequence.err)
	}
	history.revision = uint64(len(history.entries))
	return ctx.Err()
}

func (r *nativeReplay[E, C]) compactionSource(model string) (CompactionSource, error) {
	window := r.history.window()
	window.Revision = uint64(len(window.Entries))
	return r.codec.CompactionSource(window, model, func() error { return r.sequence.err })
}

func (r *nativeReplay[E, C]) checkpoint(model string, record RestoreRecord, checkpoint CheckpointEnvelope) error {
	source, err := r.compactionSource(model)
	if err != nil || source != checkpoint.Source() {
		return fmt.Errorf("checkpoint source does not match native history")
	}
	expectedTasks, hasTasks := record.ExpectedTaskProgress()
	if !hasTasks || r.codec.CheckpointTasks(checkpoint, expectedTasks) != nil {
		return fmt.Errorf("checkpoint task progress source does not match committed evidence")
	}
	summary, text, err := r.codec.DecodeCheckpoint(checkpoint)
	if err != nil {
		return err
	}
	r.history.InstallCheckpoint(checkpoint, summary, text)
	return nil
}

func (r *nativeReplay[E, C]) commit(index int, record RestoreRecord) error {
	envelope, err := record.Envelope()
	if err != nil {
		return err
	}
	entry, err := r.codec.Decode(envelope)
	if err != nil {
		return fmt.Errorf("restore %s native commit %d: %w", r.codec.Name, index+1, err)
	}
	if entry.NativeRole() != record.Role() {
		return fmt.Errorf("native commit role mismatch")
	}
	if err := r.codec.ReadyFacts(entry, record.ReadyInvocations()); err != nil {
		return err
	}
	switch entry.NativeRole() {
	case domain.NativeCommitSample:
		r.questionResults, r.taskResults = record.QuestionResults(), record.TaskResults()
	case domain.NativeCommitToolOutputs:
		if err := r.outputs(entry, record.ToolOutputs()); err != nil {
			return err
		}
		r.questionResults, r.taskResults = nil, nil
	case domain.NativeCommitHookFeedback:
		expected, ok := record.StopFeedback()
		actual, err := r.sequence.rules.Feedback(entry)
		if !ok || err != nil || actual != expected {
			return fmt.Errorf("native hook feedback differs from committed evidence")
		}
	}
	entry = entry.WithNativeTurn(record.TurnID())
	if entry.NativeRole() == domain.NativeCommitUserInputs {
		a, ok := record.Association()
		metadata := entry.NativeInputMetadata()
		if !ok || metadata == nil || !metadata.Matches(a) {
			return fmt.Errorf("native input association mismatch")
		}
	}
	frozen := entry.CloneNativeEntry()
	r.history.entries = append(r.history.entries, frozen)
	// 提前计算时序，保留首错到原先读取历史前缀的时刻再报告。
	r.sequence.observe(frozen)
	return nil
}

func (r *nativeReplay[E, C]) outputs(entry E, results []tool.ModelOutput) error {
	calls, err := r.sequence.pendingCalls(r.codec.PendingUnavailable)
	if err != nil {
		return err
	}
	expected, err := r.codec.ToolOutputs(r.sequence.turn, calls, results)
	if err != nil {
		return err
	}
	expectedEnvelope, err := r.codec.Encode(expected)
	if err != nil {
		return err
	}
	actualEnvelope, err := r.codec.Encode(entry)
	if err != nil {
		return err
	}
	if !bytes.Equal(expectedEnvelope.payload, actualEnvelope.payload) {
		return fmt.Errorf("native tool output differs from committed evidence")
	}
	sample := r.history.entries[len(r.history.entries)-1]
	if err := r.codec.QuestionResults(sample, entry, r.questionResults); err != nil {
		return err
	}
	return r.codec.TaskResults(sample, entry, r.taskResults)
}

// ProjectStoredNativeHistory 使用离线身份校验、回放与只读投影，保持各阶段的取消边界。
func ProjectStoredNativeHistory[E nativeReplayEntry[E], C any](ctx context.Context, source HistorySource, family domain.ProviderFamily, wire string, records []RestoreRecord, rules NativeSequenceRules[E, C], codec NativeReplayCodec[E, C], project func([]E) domain.SemanticHistoryView) (domain.SemanticHistoryView, error) {
	if err := source.Validate(); err != nil {
		return domain.SemanticHistoryView{}, err
	}
	if source.Family != family || source.Wire != wire {
		return domain.SemanticHistoryView{}, fmt.Errorf("history source is incompatible")
	}
	if err := ctx.Err(); err != nil {
		return domain.SemanticHistoryView{}, err
	}
	var history NativeHistory[E]
	if err := RestoreNativeHistory(ctx, source.Model, records, &history, rules, codec); err != nil {
		return domain.SemanticHistoryView{}, err
	}
	view := project(history.entries)
	if err := ctx.Err(); err != nil {
		return domain.SemanticHistoryView{}, err
	}
	return view, nil
}
