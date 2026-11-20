package anthropic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/transport"
)

var _ provider.CompactionKernel = (*Conversation)(nil)

type checkpointPayload struct {
	Summary      nativeMessage            `json:"summary"`
	TaskProgress domain.TaskProgressState `json:"task_progress"`
	Generation   json.RawMessage          `json:"generation"`
}

func (conversation *Conversation) compactionSource() (provider.CompactionSource, error) {
	return nativeCompactionSource(conversation.history.snapshotWindow(), conversation.provider.config.Model)
}

func nativeCompactionSource(snapshot nativeWindowSnapshot, model string) (provider.CompactionSource, error) {
	return nativeCompactionSourceChecked(snapshot, model, func() error { return validateNativeHistory(snapshot.Entries) })
}

func nativeCompactionSourceChecked(snapshot nativeWindowSnapshot, model string, validate func() error) (provider.CompactionSource, error) {
	entries, revision := snapshot.Entries, snapshot.Revision
	checkpoint, offset, count := snapshot.Checkpoint, snapshot.Offset, snapshot.Count
	if len(entries) == offset {
		return provider.CompactionSource{}, provider.NewCompactionError(provider.CompactionNothing)
	}
	if err := validate(); err != nil {
		return provider.CompactionSource{}, err
	}
	if entries[len(entries)-1].Kind == nativeHistorySample && len(toolUses(entries[len(entries)-1].Assistant.Content)) != 0 {
		return provider.CompactionSource{}, fmt.Errorf("compaction cannot cover pending tool calls")
	}
	predecessor := provider.RootCompactionPredecessor()
	if checkpoint != nil {
		predecessor, _ = provider.AfterCompactionCheckpoint(checkpoint.ID())
	}
	items := make([]nativeMessage, 0)
	for _, entry := range snapshot.RequestEntries() {
		if entry.FeedbackInput != nil {
			items = append(items, *entry.FeedbackInput)
		}
		items = append(items, entry.UserInputs...)
		if entry.Kind == nativeHistorySample {
			items = append(items, entry.Assistant)
		}
		if entry.Kind == nativeHistoryToolOutputs {
			items = append(items, entry.ToolOutputs)
		}
	}
	canonical, err := codec.MarshalCanonical(items, provider.MaxNativeCommitBytes)
	if err != nil {
		return provider.CompactionSource{}, err
	}
	digest := sha256.Sum256(canonical.Bytes())
	return provider.NewCompactionSource(domain.ProviderAnthropic, messagesWire, model, revision, revision+count, predecessor, hex.EncodeToString(digest[:]))
}

func (conversation *Conversation) compileCompactionRequest(input provider.CompactionInput) (codec.CanonicalJSON, error) {
	plan, err := conversation.planCompactionRequest(input)
	if err != nil {
		return codec.CanonicalJSON{}, err
	}
	return plan.body(conversation.provider.config.CachePolicy.Anthropic())
}

func (conversation *Conversation) planCompactionRequest(input provider.CompactionInput) (samplingRequestPlan, error) {
	if err := input.Validate(); err != nil {
		return samplingRequestPlan{}, err
	}
	t := input.Sampling().InferenceSettings().Thinking()
	if t.Mode() == domain.ThinkingEnabled && t.BudgetTokens() >= input.OutputTokens() {
		return samplingRequestPlan{}, provider.NewCompactionError(provider.CompactionBudgetExceeded)
	}
	plan, err := conversation.planSamplingRequest(conversation.history.snapshot(), input.Sampling())
	if err != nil {
		return samplingRequestPlan{}, err
	}
	plan.purpose = domain.RequestSummary
	plan.request.ToolChoice = &summaryToolChoice{Type: "none"}
	plan.request.MaxTokens = int(input.OutputTokens())
	plan.request.Messages = append(plan.request.Messages, newUserMessage(provider.CompactionPrompt(input.Instructions())))
	return plan, nil
}

// PreflightCompaction 只编译和估算当前完整有效窗口，不产生副作用。
func (conversation *Conversation) PreflightCompaction(input provider.CompactionInput) (provider.CompactionPreview, error) {
	return provider.PreflightCompaction(input, &conversation.active, &conversation.pending, conversation.compactionSource, conversation.compileCompactionRequest)
}

// RunCompaction 独立消费摘要流，不发普通 turn 事件，也不调用工具调度器。
func (conversation *Conversation) RunCompaction(ctx context.Context, id domain.CompactionID, preview provider.CompactionPreview) (*provider.PreparedCompaction, error) {
	return provider.RunCompaction(ctx, id, preview, &conversation.active, &conversation.pending, conversation.compactionSource, conversation.openCompaction)
}

func (conversation *Conversation) openCompaction(ctx context.Context, input provider.CompactionInput) (provider.CompactionStream, error) {
	plan, err := conversation.planCompactionRequest(input)
	if err != nil {
		return provider.CompactionStream{}, err
	}
	request, err := plan.body(conversation.provider.config.CachePolicy.Anthropic())
	if err != nil {
		return provider.CompactionStream{}, provider.NewCompactionError(provider.CompactionRequestFailed)
	}
	evidence, err := plan.evidence()
	if err != nil {
		return provider.CompactionStream{}, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := conversation.startRequest(streamCtx, request)
	if err != nil {
		err = provider.CompactionNetworkError(ctx)
		cancel()
		return provider.CompactionStream{}, err
	}
	catalog, _, _ := input.Sampling().ToolCatalog()
	reducer := newStreamReducer(catalog)
	return provider.CompactionStream{
		Messages: stream, Cancel: cancel,
		Reduce: func(event transport.SSEEvent) (bool, error) {
			result, err := reducer.reduce(event)
			if err != nil || (result.native != nil && result.native.Type == blockTypeToolUse) {
				return false, provider.NewCompactionError(provider.CompactionInvalidResponse)
			}
			return result.complete, nil
		},
		Checkpoint: func(id domain.CompactionID, source provider.CompactionSource) (*provider.PreparedCompaction, error) {
			generation := nativeHistoryEntry{Kind: nativeHistorySample, Assistant: reducer.assistantMessage(), Metadata: reducer.messageMetadata()}
			receipt, err := conversation.describeRequest(request, evidence, plan.purpose)
			if err != nil {
				return nil, err
			}
			return conversation.prepareCheckpoint(input.Scope(), id, source, generation, receipt, input.TaskProgress())
		},
	}, nil
}

func compactionSummary(generation nativeHistoryEntry) (domain.ContextSummary, error) {
	if err := validateNativeHistoryEntry(generation); err != nil {
		return domain.ContextSummary{}, provider.NewCompactionError(provider.CompactionInvalidResponse)
	}
	if !generation.Metadata.StopReason.Known || generation.Metadata.StopReason.Value != "end_turn" {
		return domain.ContextSummary{}, provider.NewCompactionError(provider.CompactionInvalidResponse)
	}
	for _, block := range generation.Assistant.Content {
		switch block.Type {
		case blockTypeText, blockTypeThinking, blockTypeRedactedThinking:
		default:
			return domain.ContextSummary{}, provider.NewCompactionError(provider.CompactionInvalidResponse)
		}
	}
	summary, err := domain.NewContextSummary(projectMessageText(generation.Assistant, roleAssistant))
	if err != nil {
		return domain.ContextSummary{}, provider.NewCompactionError(provider.CompactionInvalidResponse)
	}
	return summary, nil
}

func (conversation *Conversation) prepareCheckpoint(scope domain.CompactionScope, id domain.CompactionID, source provider.CompactionSource, generation nativeHistoryEntry, receipt domain.RequestReceipt, tasks domain.TaskProgressState) (*provider.PreparedCompaction, error) {
	summary, err := compactionSummary(generation)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeNativeCommit(generation)
	if err != nil {
		return nil, err
	}
	wrapped, err := provider.WrapScopedTaskContextSummary(summary, scope, tasks)
	if err != nil {
		return nil, err
	}
	native := newUserMessage(wrapped)
	payload, err := codec.MarshalStable(checkpointPayload{Summary: native, Generation: encoded.Payload(), TaskProgress: tasks.Clone()})
	if err != nil {
		return nil, err
	}
	summaryEntry := nativeHistoryEntry{Kind: nativeHistoryUserInputs, UserInputs: []nativeMessage{native}}
	return provider.PrepareCheckpoint(scope, id, source, receipt, &conversation.pending, provider.CheckpointCandidate{
		Family: conversation.Family(), Payload: payload, Summary: summary,
		Usage: generation.Metadata.Usage.normalized,
		Footprint: func(revision uint64) (domain.NativeHistoryFootprint, error) {
			return historyFootprint([]nativeHistoryEntry{summaryEntry}, revision)
		},
		Source:      conversation.compactionSource,
		EncodeUsage: func() ([]byte, error) { return codec.MarshalStable(encodeMetadata(generation.Metadata).Usage) },
		Install: func(checkpoint provider.CheckpointEnvelope) {
			conversation.baseline = provider.RequestBaseline{}
			conversation.nextReason = domain.ResetCompacted
			conversation.history.installCheckpoint(checkpoint, summaryEntry, summary)
		},
	})
}

func decodeCheckpoint(checkpoint provider.CheckpointEnvelope) (nativeHistoryEntry, domain.ContextSummary, error) {
	if checkpoint.Source().Family() != domain.ProviderAnthropic || checkpoint.Source().Wire() != messagesWire {
		return nativeHistoryEntry{}, domain.ContextSummary{}, fmt.Errorf("checkpoint boundary is incompatible")
	}
	var payload checkpointPayload
	if err := codec.DecodeObject(checkpoint.Payload(), &payload, "summary", "generation", "task_progress"); err != nil {
		return nativeHistoryEntry{}, domain.ContextSummary{}, fmt.Errorf("checkpoint payload is invalid")
	}
	if err := payload.TaskProgress.Validate(); err != nil {
		return nativeHistoryEntry{}, domain.ContextSummary{}, fmt.Errorf("checkpoint task progress is invalid")
	}
	envelope, err := provider.NewNativeCommitEnvelope(domain.ProviderAnthropic, messagesWire, payload.Generation)
	if err != nil {
		return nativeHistoryEntry{}, domain.ContextSummary{}, err
	}
	generation, err := decodeNativeCommit(envelope)
	if err != nil {
		return nativeHistoryEntry{}, domain.ContextSummary{}, err
	}
	summary, err := compactionSummary(generation)
	if err != nil {
		return nativeHistoryEntry{}, domain.ContextSummary{}, fmt.Errorf("checkpoint summary does not match generation")
	}
	wrapped, err := provider.WrapScopedTaskContextSummary(summary, checkpoint.Scope(), payload.TaskProgress)
	if err != nil {
		return nativeHistoryEntry{}, domain.ContextSummary{}, err
	}
	expected, err := codec.MarshalCanonical(newUserMessage(wrapped), provider.MaxNativeCommitBytes)
	if err != nil {
		return nativeHistoryEntry{}, domain.ContextSummary{}, err
	}
	actual, err := codec.MarshalCanonical(payload.Summary, provider.MaxNativeCommitBytes)
	if err != nil || string(expected.Bytes()) != string(actual.Bytes()) {
		return nativeHistoryEntry{}, domain.ContextSummary{}, fmt.Errorf("checkpoint native summary is invalid")
	}
	return nativeHistoryEntry{Kind: nativeHistoryUserInputs, UserInputs: []nativeMessage{payload.Summary.clone()}}, summary, nil
}

// ValidateCompactionUsage 验证失败计量的 raw facts 与归一化值一致。
func (instance *Provider) ValidateCompactionUsage(envelope provider.NativeCommitEnvelope, expected domain.SampleUsage) error {
	return validateCompactionUsage(envelope, expected)
}

func validateCompactionUsage(envelope provider.NativeCommitEnvelope, expected domain.SampleUsage) error {
	if envelope.Family() != domain.ProviderAnthropic || envelope.Wire() != messagesWire || len(envelope.Payload()) > 64<<10 {
		return fmt.Errorf("compaction usage boundary is incompatible")
	}
	var record rawUsageCommit
	if err := codec.UnmarshalRecord(envelope.Payload(), &record); err != nil {
		return fmt.Errorf("compaction usage encoding is invalid")
	}
	for _, field := range []optionalUintCommit{record.InputTokens, record.CacheCreationInputTokens, record.CacheReadInputTokens, record.OutputTokens, record.ThinkingTokens} {
		if field.Known != (field.Value != nil) {
			return fmt.Errorf("compaction raw usage state is invalid")
		}
	}
	raw := decodeMetadata(messageMetadataCommit{Usage: record}).Usage
	normalized, err := raw.normalized()
	if err != nil || normalized != expected {
		return fmt.Errorf("compaction usage does not match provenance")
	}
	return nil
}

// ProjectCheckpoint 纯内存解码摘要详情，不重放生成 provenance。
func (instance *Provider) ProjectCheckpoint(checkpoint provider.CheckpointEnvelope) (domain.ContextSummary, error) {
	if checkpoint.Source().Model() != instance.config.Model {
		return domain.ContextSummary{}, fmt.Errorf("checkpoint model mismatch")
	}
	_, summary, err := decodeCheckpoint(checkpoint)
	return summary, err
}

// ValidateCheckpointUsage 对照生成 provenance 核验共享成功计量。
func (instance *Provider) ValidateCheckpointUsage(checkpoint provider.CheckpointEnvelope, expected domain.SampleUsage) error {
	return validateCheckpointUsage(checkpoint, expected)
}

func validateCheckpointUsage(checkpoint provider.CheckpointEnvelope, expected domain.SampleUsage) error {
	if _, _, err := decodeCheckpoint(checkpoint); err != nil {
		return err
	}
	var payload checkpointPayload
	if err := codec.DecodeObject(checkpoint.Payload(), &payload, "summary", "generation", "task_progress"); err != nil {
		return fmt.Errorf("checkpoint usage encoding is invalid")
	}
	envelope, err := provider.NewNativeCommitEnvelope(domain.ProviderAnthropic, messagesWire, payload.Generation)
	if err != nil {
		return err
	}
	generation, err := decodeNativeCommit(envelope)
	if err != nil {
		return err
	}
	actual, err := generation.Metadata.Usage.normalized()
	if err != nil || actual != expected {
		return fmt.Errorf("checkpoint usage does not match provenance")
	}
	return nil
}

// validateCheckpointTaskProgress 核对 journal 来源前缀给出的证据，不信任自洽的包装正文。
func validateCheckpointTaskProgress(checkpoint provider.CheckpointEnvelope, expected domain.TaskProgressState) error {
	if err := expected.Validate(); err != nil {
		return err
	}
	var payload checkpointPayload
	if err := codec.DecodeObject(checkpoint.Payload(), &payload, "summary", "generation", "task_progress"); err != nil {
		return fmt.Errorf("checkpoint task progress evidence is invalid")
	}
	if payload.TaskProgress.Validate() != nil || !payload.TaskProgress.Equal(expected) {
		return fmt.Errorf("checkpoint task progress differs from committed source")
	}
	return nil
}
