package runtime

import (
	"context"

	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/session"
)

func (state *agentLoopRunState) renameSession(request loopCommandRequest) {
	if state.active != nil || state.compaction != nil || len(state.queue) != 0 {
		state.reject(request, protocol.ControlErrorSessionBusy, true)
		return
	}
	title := request.command.SessionTitle()
	draft, err := session.NewSessionTitleUpdatedDraft(title)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidInput, true)
		return
	}
	requestID := request.command.RequestID()
	select {
	case <-request.cancelled:
		request.result <- loopCommandResponse{err: fault.New(fault.CodeUserCancelled, "command admission was cancelled")}
		state.completeRequest(requestID)
		return
	case <-request.hostStopped:
		state.reject(request, protocol.ControlErrorSessionClosing, true)
		return
	case <-state.ctx.Done():
		state.reject(request, protocol.ControlErrorSessionClosing, true)
		return
	default:
		// 通过空闲与关闭检查即接纳，随后等待原 writer 的确定结果。
	}
	if title != state.title {
		if _, err := state.loop.runtime.journal.AppendBatch(context.WithoutCancel(state.ctx), []session.RecordDraft{draft}); err != nil {
			state.runErr = fault.New(fault.CodeSessionWrite, "session title save could not be confirmed")
			state.mode = loopModeClosing
			state.reject(request, protocol.ControlErrorSessionFailed, true)
			return
		}
		state.title = title
	}
	result, _ := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionUpdated)
	state.emitResult(request, result)
	state.completeRequest(requestID)
}
