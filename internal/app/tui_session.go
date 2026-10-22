package app

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	chatRuntime "easycode/internal/runtime"
)

// tuiControlSession 把Bubble Tea会话操作统一交给AgentLoop单owner。
type tuiControlSession struct {
	loop   *chatRuntime.AgentLoop
	nextID atomic.Uint64

	mu           sync.Mutex
	streams      map[protocol.RequestID]chan protocol.Event
	activeTurnID domain.TurnID
}

// startTUIControlSession 显式启动AgentLoop与唯一输出分发owner。
func startTUIControlSession(ctx context.Context, runtimeValue *chatRuntime.Runtime) (*tuiControlSession, error) {
	loop, err := chatRuntime.NewAgentLoop(runtimeValue, chatRuntime.DefaultAgentLoopConfig())
	if err != nil {
		return nil, err
	}
	runDone := make(chan error, 1)
	go func() { runDone <- loop.Run(ctx) }()
	select {
	case <-loop.Ready():
	case err := <-runDone:
		return nil, errorsOrTurnFailure(err, "TUI control loop did not start")
	case <-ctx.Done():
		return nil, fault.Wrap(fault.CodeUserCancelled, "TUI control loop start was cancelled", ctx.Err())
	}
	session := &tuiControlSession{loop: loop, streams: make(map[protocol.RequestID]chan protocol.Event)}
	go session.dispatchOutputs()
	return session, nil
}

func errorsOrTurnFailure(err error, message string) error {
	if err != nil {
		return err
	}
	return fault.New(fault.CodeTurnFailed, message)
}

func (session *tuiControlSession) requestID(prefix string) protocol.RequestID {
	value, _ := protocol.ParseRequestID(fmt.Sprintf("%s-%d", prefix, session.nextID.Add(1)))
	return value
}

func (session *tuiControlSession) Submit(ctx context.Context, text string) (<-chan protocol.Event, error) {
	requestID := session.requestID("tui-input")
	command, err := protocol.NewSubmitInputCommand(requestID, text)
	if err != nil {
		return nil, fault.New(fault.CodeInvalidInput, "TUI input is invalid")
	}
	events := make(chan protocol.Event, 16)
	session.mu.Lock()
	session.streams[requestID] = events
	session.mu.Unlock()
	result, err := session.loop.Submit(ctx, command)
	if err != nil || result.Status() != protocol.CommandStatusAccepted {
		session.mu.Lock()
		delete(session.streams, requestID)
		session.mu.Unlock()
		close(events)
		if err != nil {
			return nil, err
		}
		return nil, fault.New(fault.CodeTurnFailed, "TUI input was rejected")
	}
	return events, nil
}

func (session *tuiControlSession) ResolveApproval(ctx context.Context, turnID domain.TurnID, request protocol.ToolApprovalRequestedPayload, decision protocol.ApprovalDecision, feedback string) error {
	command, err := protocol.NewResolveToolApprovalCommand(
		session.requestID("tui-approval"), turnID, request.RequestID, request.InvocationID,
		request.Fingerprint, decision, feedback,
	)
	if err != nil {
		return fault.New(fault.CodeInvalidInput, "TUI approval response is invalid")
	}
	result, err := session.loop.Submit(ctx, command)
	if err != nil {
		return err
	}
	if result.Status() != protocol.CommandStatusAccepted {
		return fault.New(fault.CodeInvalidInput, "TUI approval response was rejected")
	}
	return nil
}

func (session *tuiControlSession) Interrupt() {
	session.mu.Lock()
	turnID := session.activeTurnID
	session.mu.Unlock()
	if !turnID.Valid() {
		return
	}
	command, err := protocol.NewInterruptCommand(session.requestID("tui-interrupt"), turnID)
	if err == nil {
		_, _ = session.loop.Submit(context.Background(), command)
	}
}

func (session *tuiControlSession) dispatchOutputs() {
	for item := range session.loop.Outputs() {
		event, ok := item.TurnEvent()
		if !ok {
			continue
		}
		session.mu.Lock()
		stream := session.streams[item.RequestID()]
		if event.Kind == protocol.EventTurnStarted {
			session.activeTurnID = event.TurnID
		}
		terminal := event.Kind == protocol.EventTurnCompleted || event.Kind == protocol.EventTurnFailed
		if terminal {
			delete(session.streams, item.RequestID())
			if session.activeTurnID == event.TurnID {
				session.activeTurnID = ""
			}
		}
		session.mu.Unlock()
		if stream != nil {
			stream <- event
			if terminal {
				close(stream)
			}
		}
	}
	session.mu.Lock()
	for requestID, stream := range session.streams {
		close(stream)
		delete(session.streams, requestID)
	}
	session.activeTurnID = ""
	session.mu.Unlock()
}

func (session *tuiControlSession) stop(ctx context.Context) error {
	if session == nil || session.loop == nil {
		return nil
	}
	return session.loop.Stop(ctx)
}
