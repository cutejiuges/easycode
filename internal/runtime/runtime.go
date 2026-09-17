// Package runtime 实现共享 turn 模板和运行时编排。
package runtime

import (
	"context"

	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
)

// Emitter 接收 Runtime 产生的共享语义事件。
type Emitter func(protocol.Event)

// Runtime 只依赖小型 Provider Kernel，不依赖具体 wire 实现。
type Runtime struct {
	provider provider.Kernel
}

// New 创建共享 Runtime。
func New(kernel provider.Kernel) *Runtime {
	return &Runtime{provider: kernel}
}

// RunTurn 执行最小 turn 模板，并将 provider 事件转发给宿主。
func (runtime *Runtime) RunTurn(
	ctx context.Context,
	input provider.TurnInput,
	emit Emitter,
) error {
	if runtime.provider == nil {
		return fault.New(fault.CodeProviderUnavailable, "provider is not configured")
	}
	if emit == nil {
		return fault.New(fault.CodeTurnFailed, "event emitter is required")
	}

	emit(protocol.NewEvent(protocol.EventTurnStarted))
	stream, err := runtime.provider.Stream(ctx, input)
	if err != nil {
		emit(protocol.NewEvent(protocol.EventTurnFailed))
		return err
	}

	for {
		select {
		case <-ctx.Done():
			emit(protocol.NewEvent(protocol.EventTurnFailed))
			return ctx.Err()
		case event, open := <-stream:
			if !open {
				emit(protocol.NewEvent(protocol.EventTurnCompleted))
				return nil
			}
			if event.Err != nil {
				emit(protocol.NewEvent(protocol.EventTurnFailed))
				return event.Err
			}
			if event.Event.Kind != "" {
				emit(event.Event)
			}
		}
	}
}
