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
	return waitLoopCommandResponse(request.result, loop.done)
}

func waitLoopCommandResponse(results <-chan loopCommandResponse, done <-chan struct{}) (protocol.CommandResult, error) {
	select {
	case response := <-results:
		return response.result, response.err
	default:
	}
	select {
	case response := <-results:
		return response.result, response.err
	case <-done:
		select {
		case response := <-results:
			return response.result, response.err
		default:
			return protocol.CommandResult{}, fault.New(fault.CodeTurnFailed, "agent loop closed before command result")
		}
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

type agentLoopRunState struct {
	loop           *AgentLoop
	ctx            context.Context
	mode           loopMode
	queue          []queuedInput
	queuedBytes    int
	outstanding    map[protocol.RequestID]struct{}
	recent         recentRequestLedger
	active         *activeLoopTurn
	runErr         error
	runContextDone <-chan struct{}
}

func newAgentLoopRunState(loop *AgentLoop, ctx context.Context) *agentLoopRunState {
	return &agentLoopRunState{
		loop: loop, ctx: ctx, mode: loopModeAccepting,
		queue:       make([]queuedInput, 0, loop.config.MaxQueuedInputs),
		outstanding: make(map[protocol.RequestID]struct{}, loop.config.MaxQueuedInputs+1),
		recent:      newRecentRequestLedger(loop.config.RecentRequests), runContextDone: ctx.Done(),
	}
}

func (state *agentLoopRunState) completeRequest(requestID protocol.RequestID) {
	delete(state.outstanding, requestID)
	state.recent.add(requestID)
}

func (state *agentLoopRunState) emitResult(request loopCommandRequest, result protocol.CommandResult) {
	item, err := protocol.NewCommandResultItem(result)
	if err != nil {
		request.result <- loopCommandResponse{err: fault.New(fault.CodeTurnFailed, "command result is invalid")}
		return
	}
	state.loop.outputs <- item
	request.result <- loopCommandResponse{result: result}
}

func (state *agentLoopRunState) reject(request loopCommandRequest, code protocol.ControlErrorCode, complete bool) {
	result, err := protocol.NewRejectedCommandResult(request.command, code)
	if err != nil {
		request.result <- loopCommandResponse{err: fault.New(fault.CodeTurnFailed, "command rejection is invalid")}
		return
	}
	state.emitResult(request, result)
	if complete {
		state.completeRequest(request.command.RequestID())
	}
}

func (state *agentLoopRunState) discardQueue(code protocol.ControlErrorCode) {
	for _, input := range state.queue {
		item, err := protocol.NewInputDiscardItem(input.requestID, code)
		if err == nil {
			state.loop.outputs <- item
		}
		state.completeRequest(input.requestID)
	}
	state.queue = state.queue[:0]
	state.queuedBytes = 0
}

func (state *agentLoopRunState) startTurn(input queuedInput) *activeLoopTurn {
	turnContext, cancel := context.WithCancel(state.ctx)
	events := make(chan protocol.Event)
	finished := make(chan error, 1)
	go func() {
		err := state.loop.runtime.RunTurn(turnContext, provider.TurnInput{Text: input.text}, func(event protocol.Event) {
			events <- event
		})
		close(events)
		finished <- err
		close(finished)
	}()
	return &activeLoopTurn{input: input, cancel: cancel, events: events, finished: finished}
}

func (state *agentLoopRunState) startNext() {
	if state.active != nil || len(state.queue) == 0 || state.mode == loopModeClosing {
		return
	}
	next := state.queue[0]
	copy(state.queue, state.queue[1:])
	state.queue = state.queue[:len(state.queue)-1]
	state.queuedBytes -= len(next.text)
	state.active = state.startTurn(next)
}

func (state *agentLoopRunState) shouldExit() bool {
	return state.active == nil && (state.mode == loopModeClosing || (state.mode == loopModeDraining && len(state.queue) == 0))
}

func (state *agentLoopRunState) activeChannels() (<-chan protocol.Event, <-chan error) {
	if state.active == nil {
		return nil, nil
	}
	return state.active.events, state.active.finished
}

func (state *agentLoopRunState) handleCommand(request loopCommandRequest) {
	requestID := request.command.RequestID()
	if _, duplicate := state.outstanding[requestID]; duplicate || state.recent.contains(requestID) {
		state.reject(request, protocol.ControlErrorDuplicateRequest, false)
		return
	}
	state.outstanding[requestID] = struct{}{}

	if state.mode == loopModeClosing {
		if request.command.Kind() == protocol.CommandShutdown {
			result, _ := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionClosing)
			state.emitResult(request, result)
			state.completeRequest(requestID)
		} else {
			state.reject(request, protocol.ControlErrorSessionClosing, true)
		}
		return
	}
	if state.mode == loopModeDraining {
		state.reject(request, protocol.ControlErrorSessionClosing, true)
		return
	}

	switch request.command.Kind() {
	case protocol.CommandSubmitInput:
		input := queuedInput{requestID: requestID, text: request.command.Text()}
		if state.active == nil && len(state.queue) == 0 {
			result, _ := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionStarting)
			state.emitResult(request, result)
			state.active = state.startTurn(input)
			return
		}
		if len(state.queue) >= state.loop.config.MaxQueuedInputs || state.queuedBytes+len(input.text) > state.loop.config.MaxQueuedBytes {
			state.reject(request, protocol.ControlErrorInputQueueFull, true)
			return
		}
		state.queue = append(state.queue, input)
		state.queuedBytes += len(input.text)
		result, _ := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionQueued)
		state.emitResult(request, result)
	case protocol.CommandInterrupt:
		if state.active == nil || state.active.turnID == "" {
			state.reject(request, protocol.ControlErrorNoActiveTurn, true)
			return
		}
		if request.command.ExpectedTurnID() != state.active.turnID {
			state.reject(request, protocol.ControlErrorTurnMismatch, true)
			return
		}
		result, _ := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionInterrupting)
		state.emitResult(request, result)
		state.completeRequest(requestID)
		if !state.active.cancelRequested {
			state.active.cancelRequested = true
			state.active.cancel()
		}
	case protocol.CommandShutdown:
		result, _ := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionClosing)
		state.emitResult(request, result)
		state.completeRequest(requestID)
		state.mode = loopModeClosing
		state.discardQueue(protocol.ControlErrorSessionShutdown)
		state.cancelActive()
	}
}

func (state *agentLoopRunState) cancelActive() {
	if state.active != nil && !state.active.cancelRequested {
		state.active.cancelRequested = true
		state.active.cancel()
	}
}

func (state *agentLoopRunState) handleLifecycle(lifecycle loopLifecycleRequest) {
	switch lifecycle.kind {
	case loopLifecycleDrain:
		if state.mode == loopModeAccepting {
			state.mode = loopModeDraining
		}
	case loopLifecycleStop:
		if state.mode != loopModeClosing {
			state.mode = loopModeClosing
			state.discardQueue(protocol.ControlErrorSessionShutdown)
		}
		state.cancelActive()
	}
	close(lifecycle.accepted)
}

func (state *agentLoopRunState) handleTurnEvent(event protocol.Event, open bool) {
	if !open {
		state.active.events = nil
		return
	}
	item, err := protocol.NewCorrelatedTurnItem(state.active.input.requestID, event)
	if err != nil {
		state.runErr = fault.New(fault.CodeTurnFailed, "runtime emitted an invalid control event")
		state.mode = loopModeClosing
		state.discardQueue(protocol.ControlErrorSessionFailed)
		state.cancelActive()
		return
	}
	if event.Kind == protocol.EventTurnStarted {
		state.active.turnID = event.TurnID
	}
	if event.Kind == protocol.EventTurnCompleted || event.Kind == protocol.EventTurnFailed {
		state.active.terminalSeen = true
	}
	state.loop.outputs <- item
}

func (state *agentLoopRunState) handleTurnFinished(workerErr error, open bool) {
	if !open {
		state.active.finished = nil
		return
	}
	state.active.finished = nil
	state.active.cancel()
	if !state.active.terminalSeen && state.runErr == nil {
		state.runErr = fault.New(fault.CodeTurnFailed, "runtime turn ended without a terminal event")
		state.mode = loopModeClosing
		state.discardQueue(protocol.ControlErrorSessionFailed)
	}
	if workerErr != nil && (state.loop.runtime.poisoned.Load() || state.loop.runtime.journal.Poisoned()) {
		if state.runErr == nil {
			state.runErr = fault.New(fault.CodeSessionWrite, "session cannot continue")
		}
		state.mode = loopModeClosing
		state.discardQueue(protocol.ControlErrorSessionFailed)
	}
	state.completeRequest(state.active.input.requestID)
	state.active = nil
}

func (state *agentLoopRunState) handleContextDone() {
	state.runErr = state.ctx.Err()
	state.runContextDone = nil
	state.mode = loopModeClosing
	state.discardQueue(protocol.ControlErrorSessionShutdown)
	state.cancelActive()
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
	state := newAgentLoopRunState(loop, ctx)

	for {
		if state.active == nil {
			if state.shouldExit() {
				return state.runErr
			}
			state.startNext()
		}
		eventChannel, finishedChannel := state.activeChannels()

		select {
		case request := <-loop.requests:
			state.handleCommand(request)

		case lifecycle := <-loop.lifecycle:
			state.handleLifecycle(lifecycle)

		case event, open := <-eventChannel:
			state.handleTurnEvent(event, open)

		case workerErr, open := <-finishedChannel:
			state.handleTurnFinished(workerErr, open)

		case <-state.runContextDone:
			state.handleContextDone()
		}
	}
}
