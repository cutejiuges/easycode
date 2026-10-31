package openai

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
	Summary    NativeItem      `json:"summary"`
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
	if entries[len(entries)-1].Kind == nativeHistorySample && len(functionCalls(entries[len(entries)-1].Outputs)) != 0 {
		return provider.CompactionSource{}, fmt.Errorf("compaction cannot cover pending tool calls")
	}
	predecessor := provider.RootCompactionPredecessor()
	if checkpoint != nil {
		predecessor, _ = provider.AfterCompactionCheckpoint(checkpoint.ID())
	}
	items := make([]NativeItem, 0)
	for _, entry := range snapshot.requestEntries() {
		items = append(items, entry.UserInputs...)
		items = append(items, entry.Outputs...)
		items = append(items, entry.ToolOutputs...)
	}
	canonical, err := codec.MarshalCanonical(items, provider.MaxNativeCommitBytes)
	if err != nil {
		return provider.CompactionSource{}, err
	}
	digest := sha256.Sum256(canonical.Bytes())
	return provider.NewCompactionSource(domain.ProviderOpenAI, responsesWire, conversation.provider.config.Model, revision, revision+count, predecessor, hex.EncodeToString(digest[:]), provider.CompactionPromptRevision)
}

func (conversation *Conversation) compileCompactionRequest(input provider.CompactionInput) (codec.CanonicalJSON, error) {
	if err := input.Validate(); err != nil {
		return codec.CanonicalJSON{}, err
	}
	ordinary, err := conversation.compileSamplingRequest(conversation.history.snapshot(), input.Sampling())
	if err != nil {
		return codec.CanonicalJSON{}, err
	}
	var request responsesRequest
	if err := codec.Unmarshal(ordinary.Bytes(), &request); err != nil {
		return codec.CanonicalJSON{}, err
	}
	request.ToolChoice = "none"
	request.MaxOutputTokens = input.OutputTokens()
	request.Input = append(request.Input, NewUserItem(provider.CompactionPrompt(input.Instructions())))
	return codec.MarshalCanonical(request, maxResponsesRequestBytes)
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
	stream, err := conversation.provider.transport.StreamSSE(streamCtx, transport.SSERequest{Method: http.MethodPost, Path: "responses", Headers: map[string]string{
		"Authorization": "Bearer " + conversation.provider.config.APIKey.Reveal(), "Accept": "text/event-stream", "Content-Type": "application/json",
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
	reducer := newResponsesStreamReducer(catalog)
	for message := range stream {
		if message.Err != nil {
			return nil, compactionNetworkError(ctx)
		}
		if message.Event == nil {
			continue
		}
		if err := validateCompactionStreamEvent(*message.Event); err != nil {
			return nil, err
		}
		result, err := reducer.reduce(*message.Event)
		if err != nil {
			return nil, provider.NewCompactionError(provider.CompactionInvalidResponse)
		}
		if result.native != nil && (result.native.Type == "function_call" || result.native.Type == "custom_tool_call") {
			return nil, provider.NewCompactionError(provider.CompactionInvalidResponse)
		}
		if !result.completed {
			continue
		}
		if ctx.Err() != nil {
			return nil, compactionNetworkError(ctx)
		}
		generation := nativeHistoryEntry{Kind: nativeHistorySample, Outputs: reducer.outputItems(), Usage: reducer.rawUsage()}
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
	for _, item := range generation.Outputs {
		switch item.Type {
		case "reasoning":
		case "message":
			for _, part := range item.Content {
				if part.Type != "output_text" {
					return domain.ContextSummary{}, provider.NewCompactionError(provider.CompactionInvalidResponse)
				}
			}
		default:
			return domain.ContextSummary{}, provider.NewCompactionError(provider.CompactionInvalidResponse)
		}
	}
	summary, err := domain.NewContextSummary(projectAssistantText(generation.Outputs))
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
	native := NewUserItem(provider.WrapContextSummary(summary))
	payload, err := codec.MarshalStable(checkpointPayload{Summary: native, Generation: encoded.Payload()})
	if err != nil {
		return nil, err
	}
	checkpoint, err := provider.NewCheckpointEnvelope(id, source, nativeCommitPayloadRevision, payload)
	if err != nil {
		return nil, err
	}
	usage, err := generation.Usage.normalized()
	if err != nil {
		return nil, err
	}
	view, err := domain.NewActiveContextView(domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{}}, summary, source.ContextRevision()+1)
	if err != nil {
		return nil, err
	}
	summaryEntry := nativeHistoryEntry{Kind: nativeHistoryUserInputs, UserInputs: []NativeItem{native}}
	footprint, err := historyFootprint([]nativeHistoryEntry{summaryEntry}, view.Revision())
	if err != nil {
		return nil, err
	}
	current, err := conversation.compactionSource()
	if err != nil || current != source {
		return nil, provider.NewCompactionError(provider.CompactionSourceChanged)
	}
	usageBytes, err := codec.MarshalStable(encodeRawUsage(generation.Usage))
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
	if checkpoint.Source().Family() != domain.ProviderOpenAI || checkpoint.Source().Wire() != responsesWire || checkpoint.PayloadVersion() != nativeCommitPayloadRevision {
		return nativeHistoryEntry{}, domain.ContextSummary{}, fmt.Errorf("checkpoint boundary is incompatible")
	}
	var payload checkpointPayload
	if err := codec.UnmarshalRecord(checkpoint.Payload(), &payload); err != nil {
		return nativeHistoryEntry{}, domain.ContextSummary{}, fmt.Errorf("checkpoint payload is invalid")
	}
	envelope, err := provider.NewNativeCommitEnvelope(domain.ProviderOpenAI, responsesWire, nativeCommitPayloadRevision, payload.Generation)
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
	expected, _ := codec.MarshalCanonical(NewUserItem(provider.WrapContextSummary(summary)), provider.MaxNativeCommitBytes)
	actual, err := codec.MarshalCanonical(payload.Summary, provider.MaxNativeCommitBytes)
	if err != nil || string(expected.Bytes()) != string(actual.Bytes()) {
		return nativeHistoryEntry{}, domain.ContextSummary{}, fmt.Errorf("checkpoint native summary is invalid")
	}
	return nativeHistoryEntry{Kind: nativeHistoryUserInputs, UserInputs: []NativeItem{payload.Summary.clone()}}, summary, nil
}

// ValidateCompactionUsage 验证失败计量的 raw facts 与归一化值一致。
func (instance *Provider) ValidateCompactionUsage(envelope provider.NativeCommitEnvelope, expected domain.SampleUsage) error {
	if envelope.Family() != domain.ProviderOpenAI || envelope.Wire() != responsesWire || envelope.PayloadVersion() != nativeCommitPayloadRevision || len(envelope.Payload()) > 64<<10 {
		return fmt.Errorf("compaction usage boundary is incompatible")
	}
	var record rawUsageCommit
	if err := codec.UnmarshalRecord(envelope.Payload(), &record); err != nil {
		return fmt.Errorf("compaction usage encoding is invalid")
	}
	raw, err := decodeRawUsage(&record)
	if err != nil {
		return err
	}
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
	envelope, err := provider.NewNativeCommitEnvelope(domain.ProviderOpenAI, responsesWire, nativeCommitPayloadRevision, payload.Generation)
	if err != nil {
		return err
	}
	generation, err := decodeNativeCommit(envelope)
	if err != nil {
		return err
	}
	actual, err := generation.Usage.normalized()
	if err != nil || actual != expected {
		return fmt.Errorf("checkpoint usage does not match provenance")
	}
	return nil
}

// validateCompactionStreamEvent 收紧摘要终态，不改变普通 Responses 的归并策略。
func validateCompactionStreamEvent(event transport.SSEEvent) error {
	var envelope struct {
		Type     string `json:"type"`
		Response struct {
			Status     string          `json:"status"`
			Incomplete json.RawMessage `json:"incomplete_details"`
		} `json:"response"`
	}
	if codec.Unmarshal([]byte(event.Data), &envelope) != nil {
		return provider.NewCompactionError(provider.CompactionInvalidResponse)
	}
	switch envelope.Type {
	case "response.refusal.delta", "response.refusal.done", "response.incomplete", "response.failed":
		return provider.NewCompactionError(provider.CompactionInvalidResponse)
	case "response.completed":
		if (envelope.Response.Status != "" && envelope.Response.Status != "completed") || (len(envelope.Response.Incomplete) != 0 && string(envelope.Response.Incomplete) != "null") {
			return provider.NewCompactionError(provider.CompactionInvalidResponse)
		}
	}
	return nil
}
