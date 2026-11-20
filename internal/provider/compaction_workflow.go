package provider

import (
	"context"

	"easycode/internal/codec"
	"easycode/internal/context/estimate"
	"easycode/internal/domain"
	"easycode/internal/provider/transport"
)

// CompactionStream 绑定摘要请求的清理、原生归并及 checkpoint 准备。
type CompactionStream struct {
	Messages   <-chan transport.SSEMessage
	Cancel     context.CancelFunc
	Cleanup    func()
	Reduce     func(transport.SSEEvent) (bool, error)
	Checkpoint func(domain.CompactionID, CompactionSource) (*PreparedCompaction, error)
}

// PreflightCompaction 以当前已提交窗口编译并估算摘要请求。
func PreflightCompaction[E nativeClone[E]](
	input CompactionInput,
	operations *ConversationOperations,
	snapshot func() NativeWindow[E],
	sourceFor func(NativeWindow[E]) (CompactionSource, error),
	compile func(NativeWindow[E], CompactionInput) (codec.CanonicalJSON, error),
	project func(NativeWindow[E]) (domain.ActiveContextView, domain.NativeHistoryFootprint, error),
) (CompactionPreview, ContextWindowProjection, error) {
	if operations.Busy() {
		return CompactionPreview{}, nil, NewCompactionError(CompactionBusy)
	}
	window := snapshot()
	source, err := sourceFor(window)
	if err != nil {
		return CompactionPreview{}, nil, err
	}
	request, err := compile(window, input)
	if err != nil {
		return CompactionPreview{}, nil, err
	}
	tokens, err := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, estimate.ByteCount(uint64(request.Len())))
	if err != nil {
		return CompactionPreview{}, nil, err
	}
	preview, err := NewCompactionPreview(source, input, tokens)
	if err != nil {
		return CompactionPreview{}, nil, err
	}
	return preview, func() (domain.ActiveContextView, domain.NativeHistoryFootprint, error) {
		return project(window)
	}, nil
}

// RunCompaction 只消费摘要流，并在排空及释放传输资源后归还拥有权。
func RunCompaction[E nativeClone[E]](ctx context.Context, id domain.CompactionID, preview CompactionPreview, operations *ConversationOperations, snapshot func() NativeWindow[E], sourceFor func(NativeWindow[E]) (CompactionSource, error), open func(context.Context, NativeWindow[E], CompactionInput) (CompactionStream, error)) (*PreparedCompaction, error) {
	if !id.Valid() || preview.Validate() != nil {
		return nil, NewCompactionError(CompactionInvalidResponse)
	}
	if operations.Begin() != ConversationIdle {
		return nil, NewCompactionError(CompactionBusy)
	}
	defer operations.End()
	window := snapshot()
	source, err := sourceFor(window)
	if err != nil || source != preview.Source() {
		return nil, NewCompactionError(CompactionSourceChanged)
	}
	stream, err := open(ctx, window, preview.Input())
	if err != nil {
		return nil, err
	}
	defer stream.Cancel()
	if stream.Cleanup != nil {
		defer stream.Cleanup()
	}
	defer stopAndDrain(stream.Cancel, stream.Messages)

	for message := range stream.Messages {
		if message.Err != nil {
			return nil, CompactionNetworkError(ctx)
		}
		if message.Event == nil {
			continue
		}
		complete, err := stream.Reduce(*message.Event)
		if err != nil {
			return nil, err
		}
		if !complete {
			continue
		}
		if ctx.Err() != nil {
			return nil, CompactionNetworkError(ctx)
		}
		return stream.Checkpoint(id, source)
	}
	return nil, CompactionNetworkError(ctx)
}

// CompactionNetworkError 按调用方的上下文分类摘要请求的传输失败。
func CompactionNetworkError(ctx context.Context) error {
	if ctx.Err() == context.DeadlineExceeded {
		return NewCompactionError(CompactionTimedOut)
	}
	if ctx.Err() != nil {
		return NewCompactionError(CompactionCancelled)
	}
	return NewCompactionError(CompactionRequestFailed)
}

// CheckpointCandidate 持有准备 checkpoint 所需的原生证据和安装操作。
type CheckpointCandidate struct {
	Family      domain.ProviderFamily
	Payload     []byte
	Summary     domain.ContextSummary
	Usage       func() (domain.SampleUsage, error)
	Footprint   func(uint64) (domain.NativeHistoryFootprint, error)
	EncodeUsage func() ([]byte, error)
	Install     func(CheckpointEnvelope)
}

// PrepareCheckpoint 核对窗口和计量证据后暂存摘要，等待 durable 结果。
func PrepareCheckpoint(scope domain.CompactionScope, id domain.CompactionID, source CompactionSource, receipt domain.RequestReceipt, operations *ConversationOperations, candidate CheckpointCandidate) (*PreparedCompaction, error) {
	checkpoint, err := NewCheckpointEnvelope(scope, id, source, candidate.Payload)
	if err != nil {
		return nil, err
	}
	usage, err := candidate.Usage()
	if err != nil {
		return nil, err
	}
	view, err := domain.NewActiveContextView(domain.SemanticHistoryView{Provider: candidate.Family, Turns: []domain.SemanticTurn{}}, candidate.Summary, source.ContextRevision()+1)
	if err != nil {
		return nil, err
	}
	footprint, err := candidate.Footprint(view.Revision())
	if err != nil {
		return nil, err
	}
	usageBytes, err := candidate.EncodeUsage()
	if err != nil {
		return nil, err
	}
	provenance, err := NewNativeCommitEnvelope(source.Family(), source.Wire(), usageBytes)
	if err != nil {
		return nil, err
	}
	prepared, err := NewPreparedCompaction(checkpoint, view, footprint, usage, provenance, receipt, func() {
		candidate.Install(checkpoint)
		operations.ResolvePrepared()
	}, operations.ResolvePrepared)
	if err == nil {
		operations.HoldPrepared()
	}
	return prepared, err
}
