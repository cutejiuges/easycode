package provider

import (
	"context"
	"errors"
	"io"

	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider/transport"
	"easycode/internal/tool"
)

// SampleReduction 是单个原生帧产生的宿主投影和完成信号。
type SampleReduction struct {
	Semantic  *protocol.Event
	Reasoning ReasoningDrafts
	Complete  bool
}

// SampleStream 绑定一次已打开的流、原生归并与提交准备。
type SampleStream struct {
	Messages <-chan transport.SSEMessage
	Reduce   func(transport.SSEEvent) (SampleReduction, error)
	Prepare  func() (*PreparedSample, error)
	Cleanup  func(bool)
	// ValidateTail 只用于会在终态后主动收口已接纳帧的 transport。
	ValidateTail bool
}

// SampleStreamErrors 保留各协议已公开的诊断文案和启动取消分类。
type SampleStreamErrors struct {
	ProviderLabel        string
	MissingTerminal      string
	CancelOpenWithParent bool
}

// SamplingStrategy 提供特定协议的历史准备与请求启动。
type SamplingStrategy[E any] struct {
	History func() (E, bool)
	Plan    func(E, TurnInput, tool.CatalogSnapshot) (func(context.Context) (SampleStream, error), error)
	Errors  SampleStreamErrors
}

// StartSample 验证并开启普通采样，将流的消费和清理交给唯一的 owner。
func StartSample[E any](ctx context.Context, input TurnInput, operations *ConversationOperations, strategy SamplingStrategy[E]) (StreamEvents, error) {
	if err := input.Validate(); err != nil {
		return nil, fault.Wrap(fault.CodeTurnFailed, "turn input is invalid", err)
	}
	switch operations.Begin() {
	case ConversationActive:
		return nil, fault.New(fault.CodeTurnFailed, "conversation already has an active turn")
	case ConversationPending:
		return nil, fault.New(fault.CodeTurnFailed, "conversation has an unfinalized sample")
	}
	transferred := false
	defer func() {
		if !transferred {
			operations.End()
		}
	}()

	history, ready := strategy.History()
	if !ready {
		return nil, fault.New(fault.CodeTurnFailed, "native history is not ready for sampling")
	}
	catalog, hasCatalog, err := input.ToolCatalog()
	if err != nil || !hasCatalog {
		return nil, fault.New(fault.CodeProviderRequest, "tool catalog is required")
	}
	open, err := strategy.Plan(history, input, catalog)
	if err != nil {
		return nil, err
	}
	streamContext, cancel := context.WithCancel(ctx)
	stream, err := open(streamContext)
	if err != nil {
		cancel()
		if errors.Is(err, context.Canceled) || strategy.Errors.CancelOpenWithParent && ctx.Err() != nil {
			return nil, sampleCancellation()
		}
		return nil, fault.Wrap(fault.CodeProviderRequest, "provider request failed", err)
	}
	transferred = true
	return stream.Consume(ctx, cancel, operations, strategy.Errors), nil
}

const sampleProgressCapacity = 16

// sampleOutput 给非终态事件限额，保留一个仅供 producer 发布终态的槽位。
type sampleOutput struct {
	events   chan StreamEvent
	progress chan struct{}
}

func newSampleOutput() sampleOutput {
	return sampleOutput{
		events:   make(chan StreamEvent, sampleProgressCapacity+1),
		progress: make(chan struct{}, sampleProgressCapacity),
	}
}

func (output sampleOutput) publish(ctx context.Context, event StreamEvent) error {
	select {
	case output.progress <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-output.progress
		return err
	}
	output.events <- event
	return nil
}

func (output sampleOutput) read(yield func(StreamEvent) bool) {
	for event := range output.events {
		if !event.Kind().Terminal() {
			<-output.progress
		}
		if !yield(event) {
			return
		}
	}
}

// Consume 将普通采样归并、传输清理与最终发布交给唯一的 producer。
func (stream SampleStream) Consume(ctx context.Context, cancel context.CancelFunc, operations *ConversationOperations, messages SampleStreamErrors) StreamEvents {
	output := newSampleOutput()
	go stream.run(ctx, cancel, operations, output, messages)
	return output.read
}

func (stream SampleStream) run(ctx context.Context, cancel context.CancelFunc, operations *ConversationOperations, output sampleOutput, messages SampleStreamErrors) {
	defer close(output.events)
	kind, err := stream.collect(ctx, output, messages)
	stopAndDrain(cancel, stream.Messages)
	var prepared *PreparedSample
	if kind == StreamEventCompleted {
		prepared, err = stream.Prepare()
		if err != nil {
			kind = StreamEventFailed
		}
	}
	terminal := sampleTerminal(messages.ProviderLabel, kind, prepared, err)
	completed := terminal.Kind() == StreamEventCompleted
	if stream.Cleanup != nil {
		stream.Cleanup(completed)
	}
	if !completed && prepared != nil {
		_ = prepared.Discard()
	}
	output.events <- terminal
	operations.End()
}

func (stream SampleStream) collect(ctx context.Context, output sampleOutput, messages SampleStreamErrors) (StreamEventKind, error) {
	publish := func(event StreamEvent) error { return output.publish(ctx, event) }
	for {
		message, ok := receiveSampleMessage(ctx, stream.Messages)
		if err := ctx.Err(); err != nil {
			return StreamEventCancelled, sampleCancellation()
		}
		if !ok {
			return StreamEventFailed, fault.New(fault.CodeStreamProtocol, messages.MissingTerminal)
		}
		if message.Err != nil {
			return sampleTransportTerminal(ctx, messages.MissingTerminal, message.Err)
		}
		if message.Event == nil {
			continue
		}
		result, err := stream.Reduce(*message.Event)
		if err != nil {
			if ctx.Err() != nil {
				return StreamEventCancelled, sampleCancellation()
			}
			return StreamEventFailed, err
		}
		if err := PublishReasoning(result.Reasoning, publish); err != nil {
			return sampleTransportTerminal(ctx, messages.MissingTerminal, err)
		}
		if err := publishSampleReduction(publish, messages.ProviderLabel, result); err != nil {
			if ctx.Err() != nil {
				return StreamEventCancelled, sampleCancellation()
			}
			return StreamEventFailed, err
		}
		if result.Complete {
			return stream.acceptTerminal(ctx, messages.MissingTerminal)
		}
	}
}

func (stream SampleStream) acceptTerminal(ctx context.Context, missingTerminal string) (StreamEventKind, error) {
	if stream.ValidateTail {
		for {
			message, ok := receiveSampleMessage(ctx, stream.Messages)
			if ctx.Err() != nil {
				return StreamEventCancelled, sampleCancellation()
			}
			if !ok || errors.Is(message.Err, io.EOF) {
				break
			}
			if message.Err != nil {
				return sampleTransportTerminal(ctx, missingTerminal, message.Err)
			}
			if message.Event != nil {
				_, err := stream.Reduce(*message.Event)
				if ctx.Err() != nil {
					return StreamEventCancelled, sampleCancellation()
				}
				if err != nil {
					return StreamEventFailed, err
				}
				return StreamEventFailed, fault.New(fault.CodeStreamProtocol, "provider emitted an event after terminal")
			}
		}
	}
	if ctx.Err() != nil {
		return StreamEventCancelled, sampleCancellation()
	}
	return StreamEventCompleted, nil
}

func receiveSampleMessage(ctx context.Context, messages <-chan transport.SSEMessage) (transport.SSEMessage, bool) {
	select {
	case <-ctx.Done():
		return transport.SSEMessage{}, false
	case message, ok := <-messages:
		return message, ok
	}
}

func stopAndDrain(cancel context.CancelFunc, stream <-chan transport.SSEMessage) {
	cancel()
	for range stream {
	}
}

func publishSampleReduction(publish func(StreamEvent) error, providerLabel string, result SampleReduction) error {
	if result.Semantic != nil {
		event, err := NewSemanticStreamEvent(*result.Semantic)
		if err != nil {
			return fault.Wrap(fault.CodeStreamProtocol, providerLabel+" semantic event is invalid", err)
		}
		if err := publish(event); err != nil {
			return err
		}
	}
	return nil
}

func sampleTerminal(providerLabel string, kind StreamEventKind, prepared *PreparedSample, err error) StreamEvent {
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
	return event
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
