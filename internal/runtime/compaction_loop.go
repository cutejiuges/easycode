package runtime

import (
	"context"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

type activeLoopCompaction struct {
	id           domain.CompactionID
	requestID    protocol.RequestID
	cancel       context.CancelFunc
	events       <-chan runtimePublication
	finished     <-chan error
	terminalSeen bool
	succeeded    bool
}

func (state *agentLoopRunState) refreshContextSnapshot() {
	state.contextSnapshot, state.contextSnapshotErr = state.loop.runtime.contextSnapshot()
}

func (state *agentLoopRunState) queryContext(request loopCommandRequest) {
	if state.contextSnapshotErr != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	payload := state.contextSnapshot
	if state.active != nil {
		payload.Activity = "turn"
	} else if state.compaction != nil {
		payload.Activity = "compacting"
	} else {
		payload.Activity = "idle"
	}
	event, err := protocol.NewContextSnapshot(state.loop.runtime.sessionID, state.loop.runtime.threadID, payload)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	result, _ := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionQueried)
	state.emitResult(request, result)
	item, _ := protocol.NewCorrelatedContextItem(request.command.RequestID(), event)
	state.loop.outputs <- item
	state.completeRequest(request.command.RequestID())
}

func (state *agentLoopRunState) compactContext(request loopCommandRequest) {
	if state.active != nil || state.compaction != nil || len(state.queue) != 0 {
		state.reject(request, protocol.ControlErrorSessionBusy, true)
		return
	}
	admission, code := state.loop.runtime.preflightCompaction(request.command.Instructions())
	if code != "" {
		state.reject(request, code, true)
		return
	}
	id, err := domain.GenerateCompactionID()
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	result, err := protocol.NewAcceptedCompactionResult(request.command, id)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	state.emitResult(request, result)
	operationCtx, cancel := context.WithCancel(state.ctx)
	events := make(chan runtimePublication)
	finished := make(chan error, 1)
	state.compaction = &activeLoopCompaction{id: id, requestID: request.command.RequestID(), cancel: cancel, events: events, finished: finished}
	go func() {
		err := state.loop.runtime.runCompaction(operationCtx, id, admission, func(event protocol.Event) { events <- state.loop.runtime.publishContext(event) })
		close(events)
		finished <- err
		close(finished)
	}()
}

func (state *agentLoopRunState) cancelCompaction(request loopCommandRequest) {
	if state.compaction == nil {
		state.reject(request, protocol.ControlErrorNoActiveCompaction, true)
		return
	}
	if state.compaction.id != request.command.ExpectedCompactionID() {
		state.reject(request, protocol.ControlErrorCompactionMismatch, true)
		return
	}
	result, _ := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionCancelling)
	state.emitResult(request, result)
	state.completeRequest(request.command.RequestID())
	state.compaction.cancel()
}

func (state *agentLoopRunState) compactionChannels() (<-chan runtimePublication, <-chan error) {
	if state.compaction == nil {
		return nil, nil
	}
	return state.compaction.events, state.compaction.finished
}

func (state *agentLoopRunState) handleCompactionEvent(publication runtimePublication, open bool) {
	event := publication.event
	if !open {
		state.compaction.events = nil
		return
	}
	item, err := protocol.NewCorrelatedContextItem(state.compaction.requestID, event)
	if err != nil || event.CompactionID != state.compaction.id || state.compaction.terminalSeen {
		state.runErr = fault.New(fault.CodeTurnFailed, "compaction emitted an invalid event")
		state.mode = loopModeClosing
		state.discardQueue(protocol.ControlErrorSessionFailed)
		state.cancelActive()
		return
	}
	if event.Kind == protocol.EventCompactionCompleted || event.Kind == protocol.EventCompactionFailed {
		state.compaction.terminalSeen = true
		state.compaction.succeeded = event.Kind == protocol.EventCompactionCompleted
		state.contextSnapshot, state.contextSnapshotErr = publication.snapshot, publication.snapshotErr
	}
	state.loop.outputs <- item
}

func (state *agentLoopRunState) handleCompactionFinished(workerErr error, open bool) {
	if !open {
		state.compaction.finished = nil
		return
	}
	state.compaction.cancel()
	if !state.compaction.terminalSeen || state.loop.runtime.poisoned.Load() || state.loop.runtime.journal.Poisoned() {
		state.runErr = fault.New(fault.CodeSessionWrite, "session cannot continue after compaction")
		state.mode = loopModeClosing
		state.discardQueue(protocol.ControlErrorSessionFailed)
	} else if workerErr != nil || !state.compaction.succeeded {
		state.returnAutomatic(protocol.InputReasonFailed)
	}
	state.completeRequest(state.compaction.requestID)
	state.compaction = nil
}

// runtimePublication 将同一提交边界的事件与只读快照一起交给控制 owner。
type runtimePublication struct {
	event       protocol.Event
	snapshot    protocol.ContextSnapshotPayload
	snapshotErr error
}

func (runtime *Runtime) publishContext(event protocol.Event) runtimePublication {
	publication := runtimePublication{event: event}
	if event.Kind != protocol.EventAssistantTextDelta {
		publication.snapshot, publication.snapshotErr = runtime.contextSnapshot()
	}
	return publication
}
