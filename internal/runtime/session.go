package runtime

import (
	"context"
	"sync"

	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
)

// ChatSession 为交互宿主提供顺序事件、取消和关闭能力。
type ChatSession struct {
	runtime *Runtime

	mu     sync.Mutex
	active bool
	closed bool
	cancel context.CancelFunc
	stop   chan struct{}
	wait   sync.WaitGroup
}

// NewChatSession 创建会话 facade。
func NewChatSession(runtime *Runtime) *ChatSession {
	return &ChatSession{runtime: runtime, stop: make(chan struct{})}
}

// Submit 启动一个 turn，并返回按顺序关闭的 RuntimeEvent stream。
func (session *ChatSession) Submit(text string) (<-chan protocol.Event, error) {
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return nil, fault.New(fault.CodeTurnFailed, "chat session is closed")
	}
	if session.active {
		session.mu.Unlock()
		return nil, fault.New(fault.CodeTurnFailed, "chat session already has an active turn")
	}
	if session.runtime == nil {
		session.mu.Unlock()
		return nil, fault.New(fault.CodeProviderUnavailable, "runtime is not configured")
	}

	turnContext, cancel := context.WithCancel(context.Background())
	events := make(chan protocol.Event)
	session.active = true
	session.cancel = cancel
	session.wait.Add(1)
	session.mu.Unlock()

	go session.runTurn(turnContext, text, events)
	return events, nil
}

func (session *ChatSession) runTurn(
	ctx context.Context,
	text string,
	events chan<- protocol.Event,
) {
	defer close(events)
	defer session.wait.Done()

	var terminal *protocol.Event
	_ = session.runtime.RunTurn(ctx, provider.TurnInput{Text: text}, func(event protocol.Event) {
		if event.Kind == protocol.EventTurnCompleted || event.Kind == protocol.EventTurnFailed {
			copy := event
			terminal = &copy
			return
		}
		select {
		case events <- event:
		case <-session.stop:
		}
	})

	session.mu.Lock()
	session.active = false
	session.cancel = nil
	session.mu.Unlock()

	if terminal != nil {
		select {
		case events <- *terminal:
		case <-session.stop:
		}
	}
}

// Interrupt 请求取消当前 turn；空闲和重复调用均为幂等操作。
func (session *ChatSession) Interrupt() {
	session.mu.Lock()
	cancel := session.cancel
	session.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Shutdown 拒绝新 turn、取消活动 turn，并等待清理结束。
func (session *ChatSession) Shutdown(ctx context.Context) error {
	session.mu.Lock()
	if !session.closed {
		session.closed = true
		close(session.stop)
	}
	cancel := session.cancel
	session.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	waitDone := make(chan struct{})
	go func() {
		session.wait.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
