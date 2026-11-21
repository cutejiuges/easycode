package runtime

import (
	"context"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

// SetWorkMode 将交互宿主的模式选择与附件准备和会话交接串行化。
func (session *ChatSession) SetWorkMode(ctx context.Context, requestID protocol.RequestID, mode domain.WorkMode) (protocol.CommandResult, error) {
	command, err := protocol.NewSetWorkModeCommand(requestID, mode)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	session.mu.Lock()
	if !session.started || session.closed || !session.interactive {
		session.mu.Unlock()
		return protocol.CommandResult{}, fault.New(fault.CodeTurnFailed, "interactive session is not running")
	}
	if session.appendAdmissions != 0 || session.resumePermit != nil || session.workModePending {
		session.mu.Unlock()
		return protocol.CommandResult{}, fault.New(fault.CodeSessionBusy, "session is not idle for a work mode change")
	}
	session.workModePending = true
	loop := session.loop
	session.mu.Unlock()
	defer func() {
		session.mu.Lock()
		session.workModePending = false
		session.mu.Unlock()
	}()
	return loop.submit(ctx, command, session.stop)
}

func (state *agentLoopRunState) setWorkMode(request loopCommandRequest) {
	if state.active != nil || state.compaction != nil || len(state.queue) != 0 {
		state.reject(request, protocol.ControlErrorSessionBusy, true)
		return
	}
	runtime := state.loop.runtime
	if runtime.poisoned.Load() || runtime.journal.Poisoned() {
		state.reject(request, protocol.ControlErrorSessionFailed, true)
		return
	}
	mode, requestID := request.command.WorkMode(), request.command.RequestID()
	profile, err := state.selectedProfile.WithWorkMode(mode)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	event, err := protocol.NewWorkModeSnapshot(runtime.sessionID, runtime.threadID, mode)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	item, err := protocol.NewCorrelatedContextItem(requestID, event)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	result, err := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionUpdated)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	select {
	case <-request.cancelled:
		request.result <- loopCommandResponse{err: fault.New(fault.CodeUserCancelled, "command admission was cancelled")}
		state.completeRequest(requestID)
		return
	case <-request.hostStopped:
		state.reject(request, protocol.ControlErrorSessionClosing, true)
		return
	case <-state.runContextDone:
		state.reject(request, protocol.ControlErrorSessionClosing, true)
		return
	default:
		// 所有确认值均已验证，交换后不撤回已接纳的设置。
		state.selectedProfile = profile
	}
	state.refreshContextSnapshot()
	state.emitResult(request, result)
	state.loop.outputs <- item
	state.completeRequest(requestID)
}
