package headless

import (
	"context"
	"errors"
	"fmt"
	"io"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

// StreamControlLoop 是长期 headless 宿主所需的最小控制器接口。
type StreamControlLoop interface {
	Run(context.Context) error
	Ready() <-chan struct{}
	Submit(context.Context, protocol.Command) (protocol.CommandResult, error)
	CloseInput(context.Context) error
	Stop(context.Context) error
	Outputs() <-chan protocol.ControlItem
	Done() <-chan struct{}
}

// StreamRunConfig 固定一个长期 stream-json 会话的身份与可解除阻塞 transport。
type StreamRunConfig struct {
	SessionID    domain.SessionID
	ThreadID     domain.ThreadID
	Resumed      bool
	Input        io.ReadCloser
	Output       io.Writer
	OutputCloser io.Closer
}

// StreamRunner 协调唯一 reader、AgentLoop owner 和 stdout writer。
type StreamRunner struct {
	loop   StreamControlLoop
	config StreamRunConfig
}

// NewStreamRunner 创建不执行 I/O、不开启 goroutine 的流式 runner。
func NewStreamRunner(loop StreamControlLoop, config StreamRunConfig) (*StreamRunner, error) {
	if loop == nil || config.Input == nil || config.Output == nil || config.OutputCloser == nil {
		return nil, fmt.Errorf("stream runner transport is not configured")
	}
	if err := validateThreadIdentity(config.SessionID, config.ThreadID); err != nil {
		return nil, fmt.Errorf("stream runner identity is invalid")
	}
	return &StreamRunner{loop: loop, config: config}, nil
}

type streamReaderResult struct {
	err error
}

type streamTransportRequest struct {
	closeInput  bool
	closeOutput bool
	done        chan struct{}
}

type streamRunnerRunState struct {
	runner                   *StreamRunner
	ctx                      context.Context
	encoder                  *StreamEncoder
	projector                *StreamProjector
	transportRequests        chan<- streamTransportRequest
	transportDone            <-chan struct{}
	outputs                  <-chan protocol.ControlItem
	readerDone               <-chan streamReaderResult
	loopDone                 <-chan error
	stopDone                 <-chan error
	pendingFailure           *fault.Summary
	loopFinished             bool
	readerFinished           bool
	stopStarted              bool
	outputUnavailable        bool
	transportContextObserved bool
}

func (state *streamRunnerRunState) requestTransport(closeInput bool, closeOutput bool) {
	request := streamTransportRequest{closeInput: closeInput, closeOutput: closeOutput, done: make(chan struct{})}
	select {
	case state.transportRequests <- request:
		<-request.done
	case <-state.transportDone:
	}
}

func (state *streamRunnerRunState) requestStop() {
	if state.stopStarted || state.loopFinished {
		return
	}
	state.stopStarted = true
	stopDone := make(chan error, 1)
	state.stopDone = stopDone
	go func() { stopDone <- state.runner.loop.Stop(context.Background()) }()
}

func (state *streamRunnerRunState) setFailure(summary fault.Summary) {
	if state.pendingFailure == nil {
		copy := summary
		state.pendingFailure = &copy
	}
}

func (state *streamRunnerRunState) handleOutput(item protocol.ControlItem, open bool) {
	if !open {
		state.outputs = nil
		return
	}
	if state.outputUnavailable || (state.pendingFailure != nil && state.pendingFailure.Code == fault.CodeStreamProtocol) {
		return
	}
	wire, err := state.projector.Project(item)
	if err != nil {
		state.setFailure(fault.Summary{Code: fault.CodeStreamProtocol, Message: "stream control output is invalid"})
		state.requestTransport(true, false)
		state.requestStop()
		return
	}
	if err := state.encoder.Write(wire); err != nil {
		state.outputUnavailable = true
		state.setFailure(fault.Summary{Code: fault.CodeOutput, Message: "write headless output failed"})
		state.requestTransport(true, true)
		state.requestStop()
	}
}

func (state *streamRunnerRunState) handleReader(result streamReaderResult) {
	state.readerDone = nil
	state.readerFinished = true
	if result.err == nil {
		return
	}
	if state.ctx.Err() != nil {
		state.setFailure(fault.Summary{Code: fault.CodeUserCancelled, Message: "stream control was cancelled", Cancelled: true})
	} else {
		state.setFailure(fault.Summary{Code: fault.CodeStreamProtocol, Message: "stream input is invalid"})
	}
	state.requestTransport(true, false)
	state.requestStop()
}

func (state *streamRunnerRunState) handleLoop(err error) {
	state.loopDone = nil
	state.loopFinished = true
	state.requestTransport(true, false)
	if err == nil || state.pendingFailure != nil {
		return
	}
	if errors.Is(err, context.Canceled) {
		state.setFailure(fault.Summary{Code: fault.CodeUserCancelled, Message: "stream control was cancelled", Cancelled: true})
	} else {
		state.setFailure(fault.Project(err))
	}
}

func (state *streamRunnerRunState) handleStop(err error) {
	state.stopDone = nil
	if err != nil && state.pendingFailure == nil {
		state.setFailure(fault.Project(err))
	}
}

func (state *streamRunnerRunState) handleTransportCancelled() {
	if state.transportContextObserved {
		return
	}
	state.transportContextObserved = true
	state.setFailure(fault.Summary{Code: fault.CodeUserCancelled, Message: "stream control was cancelled", Cancelled: true})
	state.requestStop()
}

func (state *streamRunnerRunState) running() bool {
	return !state.loopFinished || state.outputs != nil || !state.readerFinished || (state.stopStarted && state.stopDone != nil)
}

// Run 在当前 goroutine 中成为 stdout 的唯一 writer，并等待全部角色清理完成。
func (runner *StreamRunner) Run(ctx context.Context) Result {
	if ctx == nil {
		return failedResult(fault.New(fault.CodeTurnFailed, "stream runner context is required"))
	}
	if runner == nil || runner.loop == nil {
		return failedResult(fault.New(fault.CodeTurnFailed, "stream runner is not configured"))
	}
	threadStarted, err := NewStreamThreadStartedEvent(
		runner.config.SessionID, runner.config.ThreadID, runner.config.Resumed,
	)
	if err != nil {
		return failedResult(fault.New(fault.CodeStreamProtocol, "stream identity is invalid"))
	}
	runContext, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	transportRequests := make(chan streamTransportRequest)
	stopTransport := make(chan struct{})
	transportDone := make(chan struct{})
	transportCancelled := make(chan struct{}, 1)
	go runner.ownTransport(ctx, transportRequests, stopTransport, transportDone, transportCancelled)
	stopTransportOwner := func() {
		close(stopTransport)
		<-transportDone
	}
	encoder := NewStreamEncoder(runner.config.Output)
	if err := encoder.Write(threadStarted); err != nil {
		stopTransportOwner()
		return outputFailure()
	}
	projectorValue, err := NewStreamProjector(runner.config.SessionID, runner.config.ThreadID)
	if err != nil {
		stopTransportOwner()
		return failedResult(fault.New(fault.CodeStreamProtocol, "stream identity is invalid"))
	}

	loopDone := make(chan error, 1)
	go func() { loopDone <- runner.loop.Run(runContext) }()

	loopFinished := false
	transportWasCancelled := false
	var loopErr error
	select {
	case <-runner.loop.Ready():
	case loopErr = <-loopDone:
		loopFinished = true
		loopDone = nil
	case <-transportCancelled:
		loopErr = ctx.Err()
		transportWasCancelled = true
	}

	readerFinished := loopFinished
	var readerDone chan streamReaderResult
	if !loopFinished {
		readerDone = make(chan streamReaderResult, 1)
		go func() {
			readerDone <- streamReaderResult{err: readStreamCommands(runContext, runner.config.Input, runner.loop)}
		}()
	}

	state := &streamRunnerRunState{
		runner: runner, ctx: ctx, encoder: encoder, projector: projectorValue,
		transportRequests: transportRequests, transportDone: transportDone,
		outputs: runner.loop.Outputs(), readerDone: readerDone, loopDone: loopDone,
		loopFinished: loopFinished, readerFinished: readerFinished,
	}

	if loopFinished && loopErr != nil {
		state.setFailure(fault.Project(loopErr))
		state.requestTransport(true, false)
	}
	if transportWasCancelled {
		state.handleTransportCancelled()
	}

	for state.running() {
		select {
		case item, open := <-state.outputs:
			state.handleOutput(item, open)

		case result := <-state.readerDone:
			state.handleReader(result)

		case loopErr = <-state.loopDone:
			state.handleLoop(loopErr)

		case stopErr := <-state.stopDone:
			state.handleStop(stopErr)

		case <-transportCancelled:
			state.handleTransportCancelled()
		}
	}

	stopTransportOwner()
	if state.pendingFailure == nil {
		return Result{Completed: true}
	}
	if state.outputUnavailable {
		return Result{Failure: *state.pendingFailure, OutputUnavailable: true}
	}
	terminal, err := NewStreamErrorEvent(*state.pendingFailure)
	if err != nil || encoder.Write(terminal) != nil {
		return outputFailure()
	}
	return Result{Failure: *state.pendingFailure, Reported: true}
}

func readStreamCommands(ctx context.Context, input io.Reader, loop StreamControlLoop) error {
	reader, err := NewStreamCommandReader(input)
	if err != nil {
		return err
	}
	for {
		command, err := reader.ReadCommand()
		if errors.Is(err, io.EOF) {
			return loop.CloseInput(ctx)
		}
		if err != nil {
			return err
		}
		result, err := loop.Submit(ctx, command)
		if err != nil {
			return err
		}
		if command.Kind() == protocol.CommandShutdown && result.Status() == protocol.CommandStatusAccepted {
			return nil
		}
	}
}

func (runner *StreamRunner) ownTransport(
	ctx context.Context,
	requests <-chan streamTransportRequest,
	stop <-chan struct{},
	done chan<- struct{},
	cancelled chan<- struct{},
) {
	defer close(done)
	inputClosed := false
	outputClosed := false
	contextDone := ctx.Done()
	closeRequested := func(closeInput bool, closeOutput bool) {
		if closeInput && !inputClosed {
			_ = runner.config.Input.Close()
			inputClosed = true
		}
		if closeOutput && !outputClosed {
			_ = runner.config.OutputCloser.Close()
			outputClosed = true
		}
	}
	for {
		select {
		case request := <-requests:
			closeRequested(request.closeInput, request.closeOutput)
			close(request.done)
		case <-contextDone:
			contextDone = nil
			closeRequested(true, true)
			select {
			case cancelled <- struct{}{}:
			default:
			}
		case <-stop:
			return
		}
	}
}
