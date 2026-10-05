package runtime

import (
	"context"
	"fmt"
	"sync/atomic"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
)

const agentLoopOutputBuffer = 16

// AgentLoopConfig 限制单个会话控制循环的非 durable 内存状态。
type AgentLoopConfig struct {
	MaxQueuedInputs int
	MaxQueuedBytes  int
	RecentRequests  int
}

// DefaultAgentLoopConfig 返回 CLI 使用的默认有界配置。
func DefaultAgentLoopConfig() AgentLoopConfig {
	return AgentLoopConfig{
		MaxQueuedInputs: 64,
		MaxQueuedBytes:  16 << 20,
		RecentRequests:  4096,
	}
}

func (config AgentLoopConfig) validate() error {
	if config.MaxQueuedInputs <= 0 {
		return fmt.Errorf("maximum queued inputs must be positive")
	}
	if config.MaxQueuedBytes <= 0 {
		return fmt.Errorf("maximum queued bytes must be positive")
	}
	if config.RecentRequests <= 0 {
		return fmt.Errorf("recent request capacity must be positive")
	}
	return nil
}

// AgentLoop 是一个 Session-bound 的单 owner 输入控制循环。
type AgentLoop struct {
	runtime *Runtime
	config  AgentLoopConfig

	requests  chan loopCommandRequest
	lifecycle chan loopLifecycleRequest
	outputs   chan protocol.ControlItem
	ready     chan struct{}
	done      chan struct{}
	state     atomic.Uint32
}

// NewAgentLoop 只在内存中构造控制器；调用方必须显式调用 Run。
func NewAgentLoop(runtime *Runtime, config AgentLoopConfig) (*AgentLoop, error) {
	if runtime == nil {
		return nil, fault.New(fault.CodeTurnFailed, "runtime is not configured")
	}
	if err := config.validate(); err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "agent loop configuration is invalid", err)
	}
	return &AgentLoop{
		runtime: runtime, config: config,
		requests: make(chan loopCommandRequest), lifecycle: make(chan loopLifecycleRequest),
		outputs: make(chan protocol.ControlItem, agentLoopOutputBuffer), ready: make(chan struct{}), done: make(chan struct{}),
	}, nil
}

// Ready 返回 Run 已取得 owner 身份后的固定信号。
func (loop *AgentLoop) Ready() <-chan struct{} {
	if loop == nil {
		return nil
	}
	return loop.ready
}

// Outputs 返回由 owner 排序并在 Run 退出时关闭的 control item 流。
func (loop *AgentLoop) Outputs() <-chan protocol.ControlItem {
	if loop == nil {
		return nil
	}
	return loop.outputs
}

// Done 返回在 turn worker 清理且 owner 退出后关闭的固定完成信号。
func (loop *AgentLoop) Done() <-chan struct{} {
	if loop == nil {
		return nil
	}
	return loop.done
}

// Submit 将命令交给 owner；context 只参与 handoff 之前的 admission 等待。
func (loop *AgentLoop) Submit(ctx context.Context, command protocol.Command) (protocol.CommandResult, error) {
	if ctx == nil {
		return protocol.CommandResult{}, fault.New(fault.CodeTurnFailed, "command context is required")
	}
	if err := ctx.Err(); err != nil {
		return protocol.CommandResult{}, fault.Wrap(fault.CodeUserCancelled, "command admission was cancelled", err)
	}
	if err := command.Validate(); err != nil {
		return protocol.CommandResult{}, fault.New(fault.CodeInvalidInput, "command is invalid")
	}
	if loop == nil || loop.state.Load() != agentLoopRunning {
		return protocol.CommandResult{}, fault.New(fault.CodeTurnFailed, "agent loop is not running")
	}
	request := loopCommandRequest{command: command, result: make(chan loopCommandResponse, 1)}
	select {
	case loop.requests <- request:
		// owner 接收即为线性化点，此后不再观察调用方 context。
	case <-ctx.Done():
		return protocol.CommandResult{}, fault.Wrap(fault.CodeUserCancelled, "command admission was cancelled", ctx.Err())
	case <-loop.done:
		return protocol.CommandResult{}, fault.New(fault.CodeTurnFailed, "agent loop is closed")
	}
	select {
	case response := <-request.result:
		return response.result, response.err
	case <-loop.done:
		return protocol.CommandResult{}, fault.New(fault.CodeTurnFailed, "agent loop closed before command result")
	}
}

// CloseInput 停止新 admission，并让已接受输入排空。
func (loop *AgentLoop) CloseInput(ctx context.Context) error {
	return loop.requestLifecycle(ctx, loopLifecycleDrain, false)
}

// Stop 请求非协议故障清理；调用超时不会夺走 owner 的清理职责。
func (loop *AgentLoop) Stop(ctx context.Context) error {
	return loop.requestLifecycle(ctx, loopLifecycleStop, true)
}

func (loop *AgentLoop) requestLifecycle(ctx context.Context, kind loopLifecycleKind, waitDone bool) error {
	if ctx == nil {
		return fault.New(fault.CodeTurnFailed, "lifecycle context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if loop == nil || loop.state.Load() != agentLoopRunning {
		if loop != nil && loop.state.Load() == agentLoopClosed {
			return nil
		}
		return fault.New(fault.CodeTurnFailed, "agent loop is not running")
	}
	request := loopLifecycleRequest{kind: kind, accepted: make(chan struct{})}
	select {
	case loop.lifecycle <- request:
	case <-ctx.Done():
		return ctx.Err()
	case <-loop.done:
		return nil
	}
	select {
	case <-request.accepted:
	case <-loop.done:
		return nil
	}
	if !waitDone {
		return nil
	}
	select {
	case <-loop.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

const (
	agentLoopNew uint32 = iota
	agentLoopRunning
	agentLoopClosed
)

type loopCommandRequest struct {
	command protocol.Command
	result  chan loopCommandResponse
}

type loopCommandResponse struct {
	result protocol.CommandResult
	err    error
}

type loopLifecycleKind uint8

const (
	loopLifecycleDrain loopLifecycleKind = iota + 1
	loopLifecycleStop
)

type loopLifecycleRequest struct {
	kind     loopLifecycleKind
	accepted chan struct{}
}

type loopMode uint8

const (
	loopModeAccepting loopMode = iota
	loopModeDraining
	loopModeClosing
)

type queuedInput struct {
	requestID protocol.RequestID
	text      string
}

type activeLoopTurn struct {
	input           queuedInput
	cancel          context.CancelFunc
	events          <-chan protocol.Event
	finished        <-chan error
	turnID          domain.TurnID
	terminalSeen    bool
	cancelRequested bool
}

type recentRequestLedger struct {
	capacity int
	order    []protocol.RequestID
	entries  map[protocol.RequestID]struct{}
}

func newRecentRequestLedger(capacity int) recentRequestLedger {
	return recentRequestLedger{capacity: capacity, order: make([]protocol.RequestID, 0, capacity), entries: make(map[protocol.RequestID]struct{}, capacity)}
}

func (ledger *recentRequestLedger) contains(requestID protocol.RequestID) bool {
	_, ok := ledger.entries[requestID]
	return ok
}

func (ledger *recentRequestLedger) add(requestID protocol.RequestID) {
	if ledger.contains(requestID) {
		return
	}
	if len(ledger.order) == ledger.capacity {
		delete(ledger.entries, ledger.order[0])
		copy(ledger.order, ledger.order[1:])
		ledger.order = ledger.order[:len(ledger.order)-1]
	}
	ledger.order = append(ledger.order, requestID)
	ledger.entries[requestID] = struct{}{}
}

// Run 在当前 goroutine 中成为唯一状态 owner，并只为活动 turn 创建一个 worker。
func (loop *AgentLoop) Run(ctx context.Context) error {
	if ctx == nil {
		return fault.New(fault.CodeTurnFailed, "agent loop context is required")
	}
	if loop == nil || loop.runtime == nil {
		return fault.New(fault.CodeTurnFailed, "agent loop is not configured")
	}
	if !loop.state.CompareAndSwap(agentLoopNew, agentLoopRunning) {
		return fault.New(fault.CodeTurnFailed, "agent loop already ran")
	}
	close(loop.ready)
	defer func() {
		loop.state.Store(agentLoopClosed)
		close(loop.outputs)
		close(loop.done)
	}()

	mode := loopModeAccepting
	queue := make([]queuedInput, 0, loop.config.MaxQueuedInputs)
	queuedBytes := 0
	outstanding := make(map[protocol.RequestID]struct{}, loop.config.MaxQueuedInputs+1)
	recent := newRecentRequestLedger(loop.config.RecentRequests)
	var active *activeLoopTurn
	var runErr error
	runContextDone := ctx.Done()

	completeRequest := func(requestID protocol.RequestID) {
		delete(outstanding, requestID)
		recent.add(requestID)
	}
	emitResult := func(request loopCommandRequest, result protocol.CommandResult) {
		item, err := protocol.NewCommandResultItem(result)
		if err != nil {
			request.result <- loopCommandResponse{err: fault.New(fault.CodeTurnFailed, "command result is invalid")}
			return
		}
		loop.outputs <- item
		request.result <- loopCommandResponse{result: result}
	}
	reject := func(request loopCommandRequest, code protocol.ControlErrorCode, complete bool) {
		result, err := protocol.NewRejectedCommandResult(request.command, code)
		if err != nil {
			request.result <- loopCommandResponse{err: fault.New(fault.CodeTurnFailed, "command rejection is invalid")}
			return
		}
		emitResult(request, result)
		if complete {
			completeRequest(request.command.RequestID())
		}
	}
	discardQueue := func(code protocol.ControlErrorCode) {
		for _, input := range queue {
			item, err := protocol.NewInputDiscardItem(input.requestID, code)
			if err == nil {
				loop.outputs <- item
			}
			completeRequest(input.requestID)
		}
		queue = queue[:0]
		queuedBytes = 0
	}
	startTurn := func(input queuedInput) *activeLoopTurn {
		turnContext, cancel := context.WithCancel(ctx)
		events := make(chan protocol.Event)
		finished := make(chan error, 1)
		go func() {
			err := loop.runtime.RunTurn(turnContext, provider.TurnInput{Text: input.text}, func(event protocol.Event) {
				events <- event
			})
			close(events)
			finished <- err
			close(finished)
		}()
		return &activeLoopTurn{input: input, cancel: cancel, events: events, finished: finished}
	}
	startNext := func() {
		if active != nil || len(queue) == 0 || mode == loopModeClosing {
			return
		}
		next := queue[0]
		copy(queue, queue[1:])
		queue = queue[:len(queue)-1]
		queuedBytes -= len(next.text)
		active = startTurn(next)
	}

	for {
		if active == nil {
			if mode == loopModeClosing || (mode == loopModeDraining && len(queue) == 0) {
				return runErr
			}
			startNext()
		}

		var eventChannel <-chan protocol.Event
		var finishedChannel <-chan error
		if active != nil {
			eventChannel = active.events
			finishedChannel = active.finished
		}

		select {
		case request := <-loop.requests:
			requestID := request.command.RequestID()
			if _, duplicate := outstanding[requestID]; duplicate || recent.contains(requestID) {
				reject(request, protocol.ControlErrorDuplicateRequest, false)
				continue
			}
			outstanding[requestID] = struct{}{}

			if mode == loopModeClosing {
				if request.command.Kind() == protocol.CommandShutdown {
					result, _ := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionClosing)
					emitResult(request, result)
					completeRequest(requestID)
				} else {
					reject(request, protocol.ControlErrorSessionClosing, true)
				}
				continue
			}
			if mode == loopModeDraining {
				reject(request, protocol.ControlErrorSessionClosing, true)
				continue
			}

			switch request.command.Kind() {
			case protocol.CommandSubmitInput:
				input := queuedInput{requestID: requestID, text: request.command.Text()}
				if active == nil && len(queue) == 0 {
					result, _ := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionStarting)
					emitResult(request, result)
					active = startTurn(input)
					continue
				}
				if len(queue) >= loop.config.MaxQueuedInputs || queuedBytes+len(input.text) > loop.config.MaxQueuedBytes {
					reject(request, protocol.ControlErrorInputQueueFull, true)
					continue
				}
				queue = append(queue, input)
				queuedBytes += len(input.text)
				result, _ := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionQueued)
				emitResult(request, result)
			case protocol.CommandInterrupt:
				if active == nil || active.turnID == "" {
					reject(request, protocol.ControlErrorNoActiveTurn, true)
					continue
				}
				if request.command.ExpectedTurnID() != active.turnID {
					reject(request, protocol.ControlErrorTurnMismatch, true)
					continue
				}
				result, _ := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionInterrupting)
				emitResult(request, result)
				completeRequest(requestID)
				if !active.cancelRequested {
					active.cancelRequested = true
					active.cancel()
				}
			case protocol.CommandShutdown:
				result, _ := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionClosing)
				emitResult(request, result)
				completeRequest(requestID)
				mode = loopModeClosing
				discardQueue(protocol.ControlErrorSessionShutdown)
				if active != nil && !active.cancelRequested {
					active.cancelRequested = true
					active.cancel()
				}
			}

		case lifecycle := <-loop.lifecycle:
			switch lifecycle.kind {
			case loopLifecycleDrain:
				if mode == loopModeAccepting {
					mode = loopModeDraining
				}
			case loopLifecycleStop:
				if mode != loopModeClosing {
					mode = loopModeClosing
					discardQueue(protocol.ControlErrorSessionShutdown)
				}
				if active != nil && !active.cancelRequested {
					active.cancelRequested = true
					active.cancel()
				}
			}
			close(lifecycle.accepted)

		case event, open := <-eventChannel:
			if !open {
				active.events = nil
				continue
			}
			item, err := protocol.NewCorrelatedTurnItem(active.input.requestID, event)
			if err != nil {
				runErr = fault.New(fault.CodeTurnFailed, "runtime emitted an invalid control event")
				mode = loopModeClosing
				discardQueue(protocol.ControlErrorSessionFailed)
				if !active.cancelRequested {
					active.cancelRequested = true
					active.cancel()
				}
				continue
			}
			if event.Kind == protocol.EventTurnStarted {
				active.turnID = event.TurnID
			}
			if event.Kind == protocol.EventTurnCompleted || event.Kind == protocol.EventTurnFailed {
				active.terminalSeen = true
			}
			loop.outputs <- item

		case workerErr, open := <-finishedChannel:
			if !open {
				active.finished = nil
				continue
			}
			active.finished = nil
			active.cancel()
			if !active.terminalSeen && runErr == nil {
				runErr = fault.New(fault.CodeTurnFailed, "runtime turn ended without a terminal event")
				mode = loopModeClosing
				discardQueue(protocol.ControlErrorSessionFailed)
			}
			if workerErr != nil && (loop.runtime.poisoned.Load() || loop.runtime.journal.Poisoned()) {
				if runErr == nil {
					runErr = fault.New(fault.CodeSessionWrite, "session cannot continue")
				}
				mode = loopModeClosing
				discardQueue(protocol.ControlErrorSessionFailed)
			}
			completeRequest(active.input.requestID)
			active = nil

		case <-runContextDone:
			runErr = ctx.Err()
			runContextDone = nil
			mode = loopModeClosing
			discardQueue(protocol.ControlErrorSessionShutdown)
			if active != nil && !active.cancelRequested {
				active.cancelRequested = true
				active.cancel()
			}
		}
	}
}
