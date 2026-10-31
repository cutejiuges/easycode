package anthropic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"easycode/internal/codec"
	"easycode/internal/context/estimate"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/transport"
)

var _ provider.CompactionKernel = (*Conversation)(nil)

type checkpointPayload struct {
	Summary    nativeMessage   `json:"summary"`
	Generation json.RawMessage `json:"generation"`
}

func (conversation *Conversation) compactionSource() (provider.CompactionSource, error) {
	snapshot := conversation.history.snapshotWindow()
	entries, revision := snapshot.entries, snapshot.revision
	checkpoint, offset, count := snapshot.checkpoint, snapshot.offset, snapshot.count
	if len(entries) == offset {
		return provider.CompactionSource{}, provider.NewCompactionError(provider.CompactionNothing)
	}
	if err := validateNativeHistory(entries); err != nil {
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
	for _, entry := range snapshot.requestEntries() {
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
	return provider.NewCompactionSource(domain.ProviderAnthropic, messagesWire, conversation.provider.config.Model, revision, revision+count, predecessor, hex.EncodeToString(digest[:]), provider.CompactionPromptRevision)
}

func (conversation *Conversation) compileCompactionRequest(input provider.CompactionInput) (codec.CanonicalJSON, error) {
	if err := input.Validate(); err != nil {
		return codec.CanonicalJSON{}, err
	}
	ordinary, err := conversation.compileSamplingRequest(conversation.history.snapshot(), input.Sampling())
	if err != nil {
		return codec.CanonicalJSON{}, err
	}
	var request messagesRequest
	if err := codec.Unmarshal(ordinary.Bytes(), &request); err != nil {
		return codec.CanonicalJSON{}, err
	}
	request.ToolChoice = &summaryToolChoice{Type: "none"}
	request.MaxTokens = int(input.OutputTokens())
	request.Messages = append(request.Messages, newUserMessage(provider.CompactionPrompt(input.Instructions())))
	return codec.MarshalCanonical(request, maxMessagesRequestBytes)
}

// PreflightCompaction 只编译和估算当前完整有效窗口，不产生副作用。
func (conversation *Conversation) PreflightCompaction(input provider.CompactionInput) (provider.CompactionPreview, error) {
	if conversation.active.Load() || conversation.pending.Load() {
		return provider.CompactionPreview{}, provider.NewCompactionError(provider.CompactionBusy)
	}
	source, err := conversation.compactionSource()
	if err != nil {
		return provider.CompactionPreview{}, err
	}
	request, err := conversation.compileCompactionRequest(input)
	if err != nil {
		return provider.CompactionPreview{}, err
	}
	return provider.NewCompactionPreview(source, input, estimate.String(string(request.Bytes())))
}

// RunCompaction 独立消费摘要流，不发普通 turn 事件，也不调用工具调度器。
func (conversation *Conversation) RunCompaction(ctx context.Context, id domain.CompactionID, preview provider.CompactionPreview) (*provider.PreparedCompaction, error) {
	if !id.Valid() || preview.Validate() != nil {
		return nil, provider.NewCompactionError(provider.CompactionInvalidResponse)
	}
	if !conversation.active.CompareAndSwap(false, true) {
		return nil, provider.NewCompactionError(provider.CompactionBusy)
	}
	defer conversation.active.Store(false)
	if conversation.pending.Load() {
		return nil, provider.NewCompactionError(provider.CompactionBusy)
	}
	source, err := conversation.compactionSource()
	if err != nil || source != preview.Source() {
		return nil, provider.NewCompactionError(provider.CompactionSourceChanged)
	}
	request, err := conversation.compileCompactionRequest(preview.Input())
	if err != nil {
		return nil, provider.NewCompactionError(provider.CompactionRequestFailed)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := conversation.provider.transport.StreamSSE(streamCtx, transport.SSERequest{Method: http.MethodPost, Path: "messages", Headers: map[string]string{
		"x-api-key": conversation.provider.config.APIKey.Reveal(), "anthropic-version": apiVersion, "Accept": "text/event-stream", "Content-Type": "application/json",
	}, Body: request}, transport.StreamOptions{IdleTimeout: conversation.provider.config.StreamIdleTimeout})
	if err != nil {
		return nil, compactionNetworkError(ctx)
	}
	defer func() {
		cancel()
		for range stream {
		}
	}()
	catalog, _, _ := preview.Input().Sampling().ToolCatalog()
	reducer := newStreamReducer(catalog)
	for message := range stream {
		if message.Err != nil {
			return nil, compactionNetworkError(ctx)
		}
		if message.Event == nil {
			continue
		}
		result, err := reducer.reduce(*message.Event)
		if err != nil {
			return nil, provider.NewCompactionError(provider.CompactionInvalidResponse)
		}
		if result.native != nil && result.native.Type == blockTypeToolUse {
			return nil, provider.NewCompactionError(provider.CompactionInvalidResponse)
		}
		if !result.complete {
			continue
		}
		if ctx.Err() != nil {
			return nil, compactionNetworkError(ctx)
		}
		generation := nativeHistoryEntry{Kind: nativeHistorySample, Assistant: reducer.assistantMessage(), Metadata: reducer.messageMetadata()}
		return conversation.prepareCheckpoint(id, source, generation)
	}
	return nil, compactionNetworkError(ctx)
}

func compactionNetworkError(ctx context.Context) error {
	if ctx.Err() == context.DeadlineExceeded {
		return provider.NewCompactionError(provider.CompactionTimedOut)
	}
	if ctx.Err() != nil {
		return provider.NewCompactionError(provider.CompactionCancelled)
	}
	return provider.NewCompactionError(provider.CompactionRequestFailed)
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

func (conversation *Conversation) prepareCheckpoint(id domain.CompactionID, source provider.CompactionSource, generation nativeHistoryEntry) (*provider.PreparedCompaction, error) {
	summary, err := compactionSummary(generation)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeNativeCommit(generation)
	if err != nil {
		return nil, err
	}
	native := newUserMessage(provider.WrapContextSummary(summary))
	payload, err := codec.MarshalStable(checkpointPayload{Summary: native, Generation: encoded.Payload()})
	if err != nil {
		return nil, err
	}
	checkpoint, err := provider.NewCheckpointEnvelope(id, source, nativeCommitPayloadRevision, payload)
	if err != nil {
		return nil, err
	}
	usage, err := generation.Metadata.Usage.normalized()
	if err != nil {
		return nil, err
	}
	view, err := domain.NewActiveContextView(domain.SemanticHistoryView{Provider: domain.ProviderAnthropic, Turns: []domain.SemanticTurn{}}, summary, source.ContextRevision()+1)
	if err != nil {
		return nil, err
	}
	summaryEntry := nativeHistoryEntry{Kind: nativeHistoryUserInputs, UserInputs: []nativeMessage{native}}
	footprint, err := historyFootprint([]nativeHistoryEntry{summaryEntry}, view.Revision())
	if err != nil {
		return nil, err
	}
	current, err := conversation.compactionSource()
	if err != nil || current != source {
		return nil, provider.NewCompactionError(provider.CompactionSourceChanged)
	}
	usageBytes, err := codec.MarshalStable(encodeMetadata(generation.Metadata).Usage)
	if err != nil {
		return nil, err
	}
	provenance, err := provider.NewNativeCommitEnvelope(source.Family(), source.Wire(), nativeCommitPayloadRevision, usageBytes)
	if err != nil {
		return nil, err
	}
	conversation.pending.Store(true)
	prepared, err := provider.NewPreparedCompaction(checkpoint, view, footprint, usage, provenance, func() {
		conversation.history.installCheckpoint(checkpoint, summaryEntry, summary)
		conversation.pending.Store(false)
	}, func() { conversation.pending.Store(false) })
	if err != nil {
		conversation.pending.Store(false)
	}
	return prepared, err
}

func (history *nativeHistory) installCheckpoint(checkpoint provider.CheckpointEnvelope, summary nativeHistoryEntry, text domain.ContextSummary) {
	history.mu.Lock()
	defer history.mu.Unlock()
	history.checkpoint = &checkpoint
	history.summaryNative = summary
	history.contextSummary = text
	history.checkpointOffset = len(history.entries)
	history.checkpointCount++
}

func decodeCheckpoint(checkpoint provider.CheckpointEnvelope) (nativeHistoryEntry, domain.ContextSummary, error) {
	if checkpoint.Source().Family() != domain.ProviderAnthropic || checkpoint.Source().Wire() != messagesWire || checkpoint.PayloadVersion() != nativeCommitPayloadRevision {
		return nativeHistoryEntry{}, domain.ContextSummary{}, fmt.Errorf("checkpoint boundary is incompatible")
	}
	var payload checkpointPayload
	if err := codec.UnmarshalRecord(checkpoint.Payload(), &payload); err != nil {
		return nativeHistoryEntry{}, domain.ContextSummary{}, fmt.Errorf("checkpoint payload is invalid")
	}
	envelope, err := provider.NewNativeCommitEnvelope(domain.ProviderAnthropic, messagesWire, nativeCommitPayloadRevision, payload.Generation)
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
	expected, _ := codec.MarshalCanonical(newUserMessage(provider.WrapContextSummary(summary)), provider.MaxNativeCommitBytes)
	actual, err := codec.MarshalCanonical(payload.Summary, provider.MaxNativeCommitBytes)
	if err != nil || string(expected.Bytes()) != string(actual.Bytes()) {
		return nativeHistoryEntry{}, domain.ContextSummary{}, fmt.Errorf("checkpoint native summary is invalid")
	}
	return nativeHistoryEntry{Kind: nativeHistoryUserInputs, UserInputs: []nativeMessage{payload.Summary.clone()}}, summary, nil
}

// ValidateCompactionUsage 验证失败计量的 raw facts 与归一化值一致。
func (instance *Provider) ValidateCompactionUsage(envelope provider.NativeCommitEnvelope, expected domain.SampleUsage) error {
	if envelope.Family() != domain.ProviderAnthropic || envelope.Wire() != messagesWire || envelope.PayloadVersion() != nativeCommitPayloadRevision || len(envelope.Payload()) > 64<<10 {
		return fmt.Errorf("compaction usage boundary is incompatible")
	}
	var record rawUsageCommit
	if err := codec.UnmarshalRecord(envelope.Payload(), &record); err != nil {
		return fmt.Errorf("compaction usage encoding is invalid")
	}
	for _, field := range []optionalUintCommit{record.InputTokens, record.CacheCreationInputTokens, record.CacheReadInputTokens, record.OutputTokens} {
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
	if _, _, err := decodeCheckpoint(checkpoint); err != nil {
		return err
	}
	var payload checkpointPayload
	if err := codec.UnmarshalRecord(checkpoint.Payload(), &payload); err != nil {
		return fmt.Errorf("checkpoint usage encoding is invalid")
	}
	envelope, err := provider.NewNativeCommitEnvelope(domain.ProviderAnthropic, messagesWire, nativeCommitPayloadRevision, payload.Generation)
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
