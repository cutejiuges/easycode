package provider

import (
	"context"
	"errors"
	"io"
	"sync/atomic"

	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider/transport"
	"easycode/internal/tool"
)

// SampleReduction 是单个原生帧产生的宿主投影和完成信号。
type SampleReduction struct {
	Semantic  *protocol.Event
	Reasoning ReasoningDrafts
	Native    NativeItem
	Complete  bool
}

// SampleStream 绑定一次已打开的流、原生归并与提交准备。
type SampleStream struct {
	Messages <-chan transport.SSEMessage
	Reduce   func(transport.SSEEvent) (SampleReduction, error)
	Prepare  func() (*PreparedSample, error)
	Cleanup  func(bool)
}

// SampleStreamErrors 保留各协议已公开的诊断文案和启动取消分类。
type SampleStreamErrors struct {
	ProviderLabel        string
	MissingTerminal      string
	CancelOpenWithParent bool
}

// SamplingStrategy 提供特定协议的历史准备与请求启动。
type SamplingStrategy[E any] struct {
	History func() ([]E, bool)
	Plan    func([]E, TurnInput, tool.CatalogSnapshot) (func(context.Context) (SampleStream, error), error)
	Errors  SampleStreamErrors
}

// StartSample 验证并开启普通采样，将流的消费和清理交给唯一的 owner。
func StartSample[E any](ctx context.Context, input TurnInput, active, pending *atomic.Bool, strategy SamplingStrategy[E]) (<-chan StreamEvent, error) {
	if err := input.Validate(); err != nil {
		return nil, fault.Wrap(fault.CodeTurnFailed, "turn input is invalid", err)
	}
	if !active.CompareAndSwap(false, true) {
		return nil, fault.New(fault.CodeTurnFailed, "conversation already has an active turn")
	}
	if pending.Load() {
		active.Store(false)
		return nil, fault.New(fault.CodeTurnFailed, "conversation has an unfinalized sample")
	}

	history, ready := strategy.History()
	if !ready {
		active.Store(false)
		return nil, fault.New(fault.CodeTurnFailed, "native history is not ready for sampling")
	}
	catalog, hasCatalog, err := input.ToolCatalog()
	if err != nil || !hasCatalog {
		active.Store(false)
		return nil, fault.New(fault.CodeProviderRequest, "tool catalog is required")
	}
	open, err := strategy.Plan(history, input, catalog)
	if err != nil {
		active.Store(false)
		return nil, err
	}
	streamContext, cancel := context.WithCancel(ctx)
	stream, err := open(streamContext)
	if err != nil {
		cancel()
		active.Store(false)
		if errors.Is(err, context.Canceled) || strategy.Errors.CancelOpenWithParent && ctx.Err() != nil {
			return nil, sampleCancellation()
		}
		return nil, fault.Wrap(fault.CodeProviderRequest, "provider request failed", err)
	}
	output := make(chan StreamEvent, 16)
	go stream.Consume(ctx, cancel, active, output, strategy.Errors)
	return output, nil
}

// Consume 单独归并普通采样事件，并在清理完成后关闭输出。
func (stream SampleStream) Consume(ctx context.Context, cancel context.CancelFunc, active *atomic.Bool, output chan<- StreamEvent, messages SampleStreamErrors) {
	success := false
	defer close(output)
	defer active.Store(false)
	if stream.Cleanup != nil {
		defer func() { stream.Cleanup(success) }()
	}
	defer cancel()

	for message := range stream.Messages {
		if message.Event != nil {
			result, err := stream.Reduce(*message.Event)
			if err != nil {
				stopAndDrain(cancel, stream.Messages)
				sendSampleTerminal(output, messages.ProviderLabel, StreamEventFailed, nil, err)
				return
			}
			if err := PublishReasoning(ctx, output, result.Reasoning); err != nil {
				stopAndDrain(cancel, stream.Messages)
				kind, terminalErr := sampleTransportTerminal(ctx, messages.MissingTerminal, err)
				sendSampleTerminal(output, messages.ProviderLabel, kind, nil, terminalErr)
				return
			}
			if err := publishSampleReduction(output, messages.ProviderLabel, result); err != nil {
				stopAndDrain(cancel, stream.Messages)
				sendSampleTerminal(output, messages.ProviderLabel, StreamEventFailed, nil, err)
				return
			}
			if result.Complete {
				stopAndDrain(cancel, stream.Messages)
				prepared, err := stream.Prepare()
				if err != nil {
					sendSampleTerminal(output, messages.ProviderLabel, StreamEventFailed, nil, err)
					return
				}
				success = true
				sendSampleTerminal(output, messages.ProviderLabel, StreamEventCompleted, prepared, nil)
				return
			}
			continue
		}
		if message.Err != nil {
			kind, terminalErr := sampleTransportTerminal(ctx, messages.MissingTerminal, message.Err)
			sendSampleTerminal(output, messages.ProviderLabel, kind, nil, terminalErr)
			return
		}
	}
	if ctx.Err() != nil {
		sendSampleTerminal(output, messages.ProviderLabel, StreamEventCancelled, nil, sampleCancellation())
		return
	}
	sendSampleTerminal(output, messages.ProviderLabel, StreamEventFailed, nil, fault.New(fault.CodeStreamProtocol, messages.MissingTerminal))
}

func stopAndDrain(cancel context.CancelFunc, stream <-chan transport.SSEMessage) {
	cancel()
	for range stream {
	}
}

func publishSampleReduction(output chan<- StreamEvent, providerLabel string, result SampleReduction) error {
	if result.Semantic != nil {
		event, err := NewSemanticStreamEvent(*result.Semantic)
		if err != nil {
			return fault.Wrap(fault.CodeStreamProtocol, providerLabel+" semantic event is invalid", err)
		}
		output <- event
	}
	if result.Native != nil {
		event, err := NewNativeStreamEvent(result.Native)
		if err != nil {
			return fault.Wrap(fault.CodeStreamProtocol, providerLabel+" native event is invalid", err)
		}
		output <- event
	}
	return nil
}

func sendSampleTerminal(output chan<- StreamEvent, providerLabel string, kind StreamEventKind, prepared *PreparedSample, err error) {
	var event StreamEvent
	var buildErr error
	switch kind {
	case StreamEventCompleted:
		event, buildErr = NewCompletedStreamEvent(prepared)
	case StreamEventCancelled:
		event, buildErr = NewCancelledStreamEvent(err)
	default:
		event, buildErr = NewFailedStreamEvent(err)
	}
	if buildErr != nil {
		event, _ = NewFailedStreamEvent(fault.Wrap(fault.CodeStreamProtocol, providerLabel+" terminal event is invalid", buildErr))
	}
	output <- event
}

func sampleCancellation() error {
	return fault.Wrap(fault.CodeUserCancelled, "turn was cancelled", context.Canceled)
}

func sampleTransportTerminal(ctx context.Context, missingTerminal string, err error) (StreamEventKind, error) {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return StreamEventCancelled, sampleCancellation()
	}
	if transport.IsIdleTimeout(err) {
		return StreamEventFailed, fault.Wrap(fault.CodeStreamIdleTimeout, "provider stream idle timeout", err)
	}
	if errors.Is(err, io.EOF) {
		return StreamEventFailed, fault.New(fault.CodeStreamProtocol, missingTerminal)
	}
	if transport.IsEventTooLarge(err) {
		return StreamEventFailed, fault.Wrap(fault.CodeStreamProtocol, "provider stream event is too large", err)
	}
	return StreamEventFailed, fault.Wrap(fault.CodeStreamProtocol, "provider stream failed", err)
}
