// Package runtime 实现共享 turn 模板和运行时编排。
package runtime

import (
	"context"
	"errors"
	"sync/atomic"

	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
)

// Emitter 接收 Runtime 产生的共享语义事件。
type Emitter func(protocol.Event)

// Runtime 持有一条会话级 Provider Conversation。
type Runtime struct {
	conversation provider.Conversation
	active       atomic.Bool
}

// New 创建共享 Runtime。
func New(conversation provider.Conversation) *Runtime {
	return &Runtime{conversation: conversation}
}

// RunTurn 执行一次文本 turn，并等待 Provider terminal 后的清理关闭。
func (runtime *Runtime) RunTurn(
	ctx context.Context,
	input provider.TurnInput,
	emit Emitter,
) error {
	if runtime.conversation == nil {
		return fault.New(fault.CodeProviderUnavailable, "provider conversation is not configured")
	}
	if emit == nil {
		return fault.New(fault.CodeTurnFailed, "event emitter is required")
	}
	if !runtime.active.CompareAndSwap(false, true) {
		return fault.New(fault.CodeTurnFailed, "conversation already has an active turn")
	}
	defer runtime.active.Store(false)

	emit(protocol.NewEvent(protocol.EventTurnStarted))
	stream, err := runtime.conversation.Stream(ctx, input)
	if err != nil {
		emitFailure(emit, err)
		return err
	}

	var terminal *provider.StreamEvent
	for event := range stream {
		if terminal != nil {
			err = fault.New(fault.CodeStreamProtocol, "provider emitted an event after terminal")
			emitFailure(emit, err)
			return err
		}

		switch event.Kind {
		case provider.StreamEventSemantic:
			if ctx.Err() == nil && event.Event.Kind != "" {
				emit(event.Event)
			}
		case provider.StreamEventNative:
			// Provider 原生 item 只由 Conversation 持有，Runtime 不做投影。
		case provider.StreamEventCompleted, provider.StreamEventFailed, provider.StreamEventCancelled:
			copy := event
			terminal = &copy
		default:
			err = fault.New(fault.CodeStreamProtocol, "provider emitted an invalid stream event")
			emitFailure(emit, err)
			return err
		}
	}

	if terminal == nil {
		err = fault.New(fault.CodeStreamProtocol, "provider stream closed before terminal")
		emitFailure(emit, err)
		return err
	}

	switch terminal.Kind {
	case provider.StreamEventCompleted:
		emit(protocol.NewEvent(protocol.EventTurnCompleted))
		return nil
	case provider.StreamEventCancelled:
		err = terminal.Err
		if err == nil {
			err = fault.Wrap(fault.CodeUserCancelled, "turn was cancelled", context.Canceled)
		}
		emitFailure(emit, err)
		return err
	case provider.StreamEventFailed:
		err = terminal.Err
		if err == nil {
			err = fault.New(fault.CodeProviderRequest, "provider request failed")
		}
		emitFailure(emit, err)
		return err
	default:
		err = fault.New(fault.CodeStreamProtocol, "provider terminal is invalid")
		emitFailure(emit, err)
		return err
	}
}

func emitFailure(emit Emitter, err error) {
	code, message, cancelled := failureSummary(err)
	event, buildErr := protocol.NewTurnFailed(string(code), message, cancelled)
	if buildErr != nil {
		return
	}
	emit(event)
}

func failureSummary(err error) (fault.Code, string, bool) {
	if errors.Is(err, context.Canceled) {
		return fault.CodeUserCancelled, "turn was cancelled", true
	}
	var faultError *fault.Error
	if errors.As(err, &faultError) {
		return faultError.Code, faultError.Message, faultError.Code == fault.CodeUserCancelled
	}
	return fault.CodeTurnFailed, "turn failed", false
}
