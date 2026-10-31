package runtime

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

// ChatSession 把交互与一次性宿主操作统一交给 AgentLoop 单 owner。
type ChatSession struct {
	runtime     *Runtime
	loop        *AgentLoop
	started     bool
	closed      bool
	cancel      context.CancelFunc
	stop        chan struct{}
	done        chan struct{}
	runErr      error
	nextID      atomic.Uint64
	interrupts  chan domain.TurnID
	interactive bool
	outputs     chan protocol.ControlItem

	mu           sync.Mutex
	streams      map[protocol.RequestID]chan protocol.Event
	activeTurnID domain.TurnID
}

// NewChatSession 只构造 AgentLoop 的宿主适配器；Start 显式取得运行所有权。
func NewChatSession(runtimeValue *Runtime) *ChatSession {
	return &ChatSession{runtime: runtimeValue, outputs: make(chan protocol.ControlItem, 16), streams: make(map[protocol.RequestID]chan protocol.Event), stop: make(chan struct{}), done: make(chan struct{}), interrupts: make(chan domain.TurnID, 1)}
}

// StartInteractive 显式选择长期控制输出并启动会话。
func (session *ChatSession) StartInteractive(ctx context.Context) error {
	session.mu.Lock()
	if session.started || session.closed {
		session.mu.Unlock()
		return fmt.Errorf("session already started or closed")
	}
	session.interactive = true
	session.mu.Unlock()
	return session.Start(ctx)
}

// Outputs 返回交互宿主独占消费的长期控制流。
func (session *ChatSession) Outputs() <-chan protocol.ControlItem { return session.outputs }

// Append 向 owner 提交可在安全边界领取的输入。
func (session *ChatSession) Append(ctx context.Context, text string) (protocol.CommandResult, error) {
	command, err := protocol.NewSubmitConversationalInputCommand(session.requestID("host-append"), text)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	return session.submitInteractive(ctx, command)
}

// Retract 原子取回尚未领取的指定输入。
func (session *ChatSession) Retract(ctx context.Context, ids []domain.InputID) (protocol.CommandResult, error) {
	command, err := protocol.NewRetractInputsCommand(session.requestID("host-retract"), ids)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	return session.submitInteractive(ctx, command)
}

func (session *ChatSession) submitInteractive(ctx context.Context, command protocol.Command) (protocol.CommandResult, error) {
	session.mu.Lock()
	live, loop := session.started && !session.closed && session.interactive, session.loop
	session.mu.Unlock()
	if !live {
		return protocol.CommandResult{}, fmt.Errorf("interactive session is not running")
	}
	return loop.Submit(ctx, command)
}

// Start 显式启动唯一控制循环与输出分发器，返回前确认启动或完成清理。
func (session *ChatSession) Start(ctx context.Context) error {
	if ctx == nil {
		return fault.New(fault.CodeTurnFailed, "session context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return fault.New(fault.CodeTurnFailed, "session already started or closed")
	}
	if session.started {
		loop := session.loop
		session.mu.Unlock()
		select {
		case <-loop.Ready():
			return nil
		case <-session.done:
			return errorsOrTurnFailure(session.runErr, "session is closed")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	loop, err := NewAgentLoop(session.runtime, DefaultAgentLoopConfig())
	if err != nil {
		session.mu.Unlock()
		return err
	}
	runContext, cancel := context.WithCancel(ctx)
	session.started = true
	session.loop = loop
	session.cancel = cancel
	session.mu.Unlock()
	go func() {
		runDone := make(chan error, 1)
		controlsDone := make(chan struct{})
		go func() { defer close(controlsDone); session.runInterrupts(runContext) }()
		go func() { runDone <- loop.Run(runContext) }()
		session.dispatchOutputs()
		session.runErr = <-runDone
		cancel()
		<-controlsDone
		if session.runErr == context.Canceled {
			session.runErr = nil
		}
		cancel()
		close(session.done)
	}()
	select {
	case <-loop.Ready():
		if ctx.Err() == nil {
			return nil
		}
	case <-session.done:
		return errorsOrTurnFailure(session.runErr, "session loop did not start")
	case <-ctx.Done():
	}
	cancel()
	<-session.done
	return ctx.Err()
}

func errorsOrTurnFailure(err error, message string) error {
	if err != nil {
		return err
	}
	return fault.New(fault.CodeTurnFailed, message)
}

func (session *ChatSession) requestID(prefix string) protocol.RequestID {
	value, _ := protocol.ParseRequestID(fmt.Sprintf("%s-%d", prefix, session.nextID.Add(1)))
	return value
}

func (session *ChatSession) Submit(ctx context.Context, text string) (<-chan protocol.Event, error) {
	if ctx == nil {
		return nil, fault.New(fault.CodeTurnFailed, "turn context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, fault.Wrap(fault.CodeUserCancelled, "turn was cancelled", err)
	}
	session.mu.Lock()
	live := session.started && !session.closed
	session.mu.Unlock()
	if !live {
		return nil, fault.New(fault.CodeTurnFailed, "session is not running")
	}

	requestID := session.requestID("host-input")
	command, err := protocol.NewSubmitInputCommand(requestID, text)
	if err != nil {
		return nil, fault.New(fault.CodeInvalidInput, "host input is invalid")
	}
	events := make(chan protocol.Event, 16)
	session.mu.Lock()
	if len(session.streams) != 0 || session.closed || session.interactive {
		session.mu.Unlock()
		return nil, fault.New(fault.CodeTurnFailed, "host already has an active input")
	}
	session.streams[requestID] = events
	session.mu.Unlock()
	result, err := session.loop.Submit(ctx, command)
	if err != nil || result.Status() != protocol.CommandStatusAccepted {
		session.mu.Lock()
		delete(session.streams, requestID)
		session.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return nil, fault.New(fault.CodeTurnFailed, "host input was rejected")
	}
	return events, nil
}

func (session *ChatSession) ResolveApproval(ctx context.Context, turnID domain.TurnID, request protocol.ToolApprovalRequestedPayload, decision protocol.ApprovalDecision, feedback string) error {
	command, err := protocol.NewResolveToolApprovalCommand(
		session.requestID("host-approval"), turnID, request.RequestID, request.InvocationID,
		request.Fingerprint, decision, feedback,
	)
	if err != nil {
		return fault.New(fault.CodeInvalidInput, "host approval response is invalid")
	}
	result, err := session.loop.Submit(ctx, command)
	if err != nil {
		return err
	}
	if result.Status() != protocol.CommandStatusAccepted {
		return fault.New(fault.CodeInvalidInput, "host approval response was rejected")
	}
	return nil
}

func (session *ChatSession) Interrupt() {
	session.mu.Lock()
	defer session.mu.Unlock()
	turnID := session.activeTurnID
	if !turnID.Valid() {
		return
	}
	// 只保留最新目标，避免旧 turn 的未交付请求占住当前 turn 的中断槽。
	select {
	case <-session.interrupts:
	default:
	}
	select {
	case session.interrupts <- turnID:
	default:
	}
}

func (session *ChatSession) dispatchOutputs() {
	defer close(session.outputs)
	for item := range session.loop.Outputs() {
		event, ok := item.TurnEvent()
		if !ok {
			if session.interactive {
				select {
				case session.outputs <- item:
				case <-session.stop:
				}
			}
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
		if session.interactive {
			select {
			case session.outputs <- item:
			case <-session.stop:
			}
		}
		if stream != nil && event.Kind != protocol.EventUserInputCommitted {
			select {
			case stream <- event:
			case <-session.stop:
			}
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

// Shutdown 停止宿主投递并取消控制循环；超时仍由固定 owner 持有清理职责。
func (session *ChatSession) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return fault.New(fault.CodeTurnFailed, "shutdown context is required")
	}
	session.mu.Lock()
	if !session.closed {
		session.closed = true
		close(session.stop)
	}
	cancel := session.cancel
	started := session.started
	session.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if !started {
		return nil
	}
	select {
	case <-session.done:
		return session.runErr
	default:
	}
	select {
	case <-session.done:
		return session.runErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Done 在控制循环和分发器均完成后关闭。
func (session *ChatSession) Done() <-chan struct{} { return session.done }

// runInterrupts 独占中断交付，宿主无需等待有背压的控制 admission 即可继续排空事件。
func (session *ChatSession) runInterrupts(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case turnID := <-session.interrupts:
			requestID := session.requestID("host-interrupt")
			command, err := protocol.NewInterruptCommand(requestID, turnID)
			if session.interactive {
				command, err = protocol.NewStopTurnCommand(requestID, turnID)
			}
			if err == nil {
				_, _ = session.loop.Submit(ctx, command)
			}
		}
	}
}

// QueryContext 查询单 owner 保存的最近已提交上下文快照。
func (session *ChatSession) QueryContext(ctx context.Context) (protocol.CommandResult, error) {
	command, err := protocol.NewQueryContextCommand(session.requestID("host-context"))
	if err != nil {
		return protocol.CommandResult{}, err
	}
	return session.submitInteractive(ctx, command)
}

// CompactContext 请求空闲时执行手动压缩。
func (session *ChatSession) CompactContext(ctx context.Context, instructions string) (protocol.CommandResult, error) {
	command, err := protocol.NewCompactContextCommand(session.requestID("host-compact"), instructions)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	return session.submitInteractive(ctx, command)
}

// CancelCompaction 取消指定压缩操作，不影响后来的 turn 或压缩。
func (session *ChatSession) CancelCompaction(ctx context.Context, id domain.CompactionID) (protocol.CommandResult, error) {
	command, err := protocol.NewCancelCompactionCommand(session.requestID("host-cancel-compaction"), id)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	return session.submitInteractive(ctx, command)
}
