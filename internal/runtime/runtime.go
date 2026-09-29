// Package runtime 实现共享 turn 模板和 durable 生命周期编排。
package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

// Emitter 接收 Runtime 产生的共享语义事件。
type Emitter func(protocol.Event)

// Journal 是 Runtime 所需的最小 durable Session 能力。
type Journal interface {
	AppendBatch(context.Context, []session.RecordDraft) ([]session.Record, error)
	Poisoned() bool
}

// TurnIDGenerator 为每个被接受的 turn 分配稳定 UUIDv7 标识。
type TurnIDGenerator func() (domain.TurnID, error)

// Config 固定一个 Session-bound Runtime 的身份与 durable 边界。
type Config struct {
	SessionID       domain.SessionID
	ThreadID        domain.ThreadID
	Journal         Journal
	NewTurnID       TurnIDGenerator
	InterruptedTail bool
}

// Runtime 持有一条会话级 Provider Conversation 和同一 thread journal。
type Runtime struct {
	conversation provider.Conversation
	journal      Journal
	sessionID    domain.SessionID
	threadID     domain.ThreadID
	newTurnID    TurnIDGenerator
	active       atomic.Bool
	poisoned     atomic.Bool
}

// New 创建已绑定稳定 Session 身份的共享 Runtime。
func New(conversation provider.Conversation, config Config) (*Runtime, error) {
	if conversation == nil {
		return nil, fault.New(fault.CodeProviderUnavailable, "provider conversation is not configured")
	}
	if !config.SessionID.Valid() || !config.ThreadID.Valid() {
		return nil, fault.New(fault.CodeTurnFailed, "runtime session identity is invalid")
	}
	if config.Journal == nil {
		return nil, fault.New(fault.CodeSessionWrite, "session journal is not configured")
	}
	if config.InterruptedTail {
		return nil, fault.New(fault.CodeSessionCorruption, "interrupted session turn must be closed before runtime starts")
	}
	if config.NewTurnID == nil {
		config.NewTurnID = domain.NewTurnID
	}
	return &Runtime{
		conversation: conversation, journal: config.Journal,
		sessionID: config.SessionID, threadID: config.ThreadID, newTurnID: config.NewTurnID,
	}, nil
}

// RunTurn 执行一次文本 turn，并等待 Provider terminal 后的 durable 收口。
func (runtime *Runtime) RunTurn(
	ctx context.Context,
	input provider.TurnInput,
	emit Emitter,
) error {
	if ctx == nil {
		return fault.New(fault.CodeTurnFailed, "turn context is required")
	}
	if emit == nil {
		return fault.New(fault.CodeTurnFailed, "event emitter is required")
	}
	if runtime == nil || runtime.conversation == nil {
		return fault.New(fault.CodeProviderUnavailable, "provider conversation is not configured")
	}
	if runtime.poisoned.Load() || runtime.journal.Poisoned() {
		return fault.New(fault.CodeSessionWrite, "session journal is poisoned")
	}
	if !runtime.active.CompareAndSwap(false, true) {
		return fault.New(fault.CodeTurnFailed, "conversation already has an active turn")
	}
	defer runtime.active.Store(false)

	turnID, err := runtime.newTurnID()
	if err != nil || !turnID.Valid() {
		if err == nil {
			err = fmt.Errorf("generated turn ID is invalid")
		}
		failure := fault.Wrap(fault.CodeTurnFailed, "turn identity generation failed", err)
		runtime.emitFailure(emit, turnID, failure)
		return failure
	}
	if _, err := runtime.journal.AppendBatch(ctx, []session.RecordDraft{{
		EventKind: session.EventTurnStarted, TurnID: turnID, Payload: session.TurnStartedPayload{},
	}}); err != nil {
		failure := fault.Wrap(fault.CodeSessionWrite, "persist turn start failed", err)
		runtime.poisoned.Store(true)
		runtime.emitFailure(emit, turnID, failure)
		return failure
	}
	emit(runtime.decorate(protocol.NewTurnStarted(), turnID))

	stream, err := runtime.conversation.Stream(ctx, input)
	if err != nil {
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	}

	var terminal *provider.StreamEvent
	for event := range stream {
		if terminal != nil {
			err = fault.New(fault.CodeStreamProtocol, "provider emitted an event after terminal")
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
		}

		switch event.Kind {
		case provider.StreamEventSemantic:
			if ctx.Err() == nil && event.Event.Kind != "" {
				emit(runtime.decorate(event.Event, turnID))
			}
		case provider.StreamEventNative:
			// Provider 原生 item 只由 Conversation staging 持有，Runtime 不解释 wire。
		case provider.StreamEventCompleted, provider.StreamEventFailed, provider.StreamEventCancelled:
			copy := event
			terminal = &copy
		default:
			err = fault.New(fault.CodeStreamProtocol, "provider emitted an invalid stream event")
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
		}
	}

	if terminal == nil {
		err = fault.New(fault.CodeStreamProtocol, "provider stream closed before terminal")
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	}

	switch terminal.Kind {
	case provider.StreamEventCompleted:
		if terminal.Prepared == nil || terminal.Prepared.Finalized() {
			err = fault.New(fault.CodeStreamProtocol, "provider completed without a fresh prepared sample")
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
		}
		envelope, envelopeErr := terminal.Prepared.Envelope()
		if envelopeErr != nil {
			err = fault.Wrap(fault.CodeStreamProtocol, "provider prepared sample is invalid", envelopeErr)
			return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
		}
		_, err = runtime.journal.AppendBatch(context.WithoutCancel(ctx), []session.RecordDraft{
			{
				EventKind: session.EventProviderNativeCommit, TurnID: turnID,
				Payload: session.NativeCommitPayload{
					Provider: envelope.Family(), Wire: envelope.Wire(),
					PayloadVersion: envelope.PayloadVersion(), Payload: envelope.Payload(),
				},
			},
			{EventKind: session.EventTurnCompleted, TurnID: turnID, Payload: session.TurnCompletedPayload{}},
		})
		if err != nil {
			failure := fault.Wrap(fault.CodeSessionWrite, "persist completed turn failed", err)
			runtime.poisoned.Store(true)
			runtime.emitFailure(emit, turnID, failure)
			return failure
		}
		if err := terminal.Prepared.Finalize(); err != nil {
			failure := fault.Wrap(fault.CodeStreamProtocol, "finalize prepared sample failed", err)
			runtime.poisoned.Store(true)
			runtime.emitFailure(emit, turnID, failure)
			return failure
		}
		emit(runtime.decorate(protocol.NewTurnCompleted(), turnID))
		return nil
	case provider.StreamEventCancelled:
		err = terminal.Err
		if err == nil || errors.Is(err, context.Canceled) {
			err = fault.Wrap(fault.CodeUserCancelled, "turn was cancelled", context.Canceled)
		}
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	case provider.StreamEventFailed:
		err = terminal.Err
		if err == nil {
			err = fault.New(fault.CodeProviderRequest, "provider request failed")
		}
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	default:
		err = fault.New(fault.CodeStreamProtocol, "provider terminal is invalid")
		return runtime.failTurn(context.WithoutCancel(ctx), emit, turnID, err)
	}
}

func (runtime *Runtime) failTurn(ctx context.Context, emit Emitter, turnID domain.TurnID, cause error) error {
	code, message, cancelled := failureSummary(cause)
	if !runtime.poisoned.Load() && !runtime.journal.Poisoned() {
		_, err := runtime.journal.AppendBatch(ctx, []session.RecordDraft{{
			EventKind: session.EventTurnFailed, TurnID: turnID,
			Payload: session.TurnFailedPayload{Code: string(code), Message: message, Cancelled: cancelled},
		}})
		if err != nil {
			failure := fault.Wrap(fault.CodeSessionWrite, "persist turn failure failed", err)
			runtime.poisoned.Store(true)
			runtime.emitFailure(emit, turnID, failure)
			return failure
		}
	}
	runtime.emitFailure(emit, turnID, cause)
	return cause
}

func (runtime *Runtime) decorate(event protocol.Event, turnID domain.TurnID) protocol.Event {
	event.SessionID = runtime.sessionID
	event.ThreadID = runtime.threadID
	event.TurnID = turnID
	return event
}

func (runtime *Runtime) emitFailure(emit Emitter, turnID domain.TurnID, err error) {
	code, message, cancelled := failureSummary(err)
	event, buildErr := protocol.NewTurnFailed(string(code), message, cancelled)
	if buildErr != nil {
		return
	}
	emit(runtime.decorate(event, turnID))
}

func failureSummary(err error) (fault.Code, string, bool) {
	summary := fault.Project(err)
	return summary.Code, summary.Message, summary.Cancelled
}
