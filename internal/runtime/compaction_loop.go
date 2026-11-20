package runtime

import (
	"context"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

type activeLoopCompaction struct {
	profile           contextplan.ProviderProfile
	selectionRevision uint64
	started           bool
	cancelRequested   bool
	id                domain.CompactionID
	requestID         protocol.RequestID
	cancel            context.CancelFunc
	events            <-chan runtimePublication
	finished          <-chan error
	terminalSeen      bool
	succeeded         bool
}

func (state *agentLoopRunState) refreshContextSnapshot() {
	state.contextSnapshot, state.contextSnapshotErr = state.loop.runtime.contextSnapshotFor(state.selectedProfile)
	if state.cacheSnapshot.Scope == "" {
		plan, err := state.loop.runtime.contextPlanFor(state.selectedProfile)
		if err != nil {
			state.cacheSnapshotErr = err
			return
		}
		state.cacheSnapshot, state.cacheSnapshotErr = state.loop.runtime.cacheSnapshotForPlan(plan)
		state.loop.runtime.cache.snapshot = state.cacheSnapshot
	}
}

func (state *agentLoopRunState) queryContext(request loopCommandRequest) {
	if state.contextSnapshotErr != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	effort, err := state.effortSnapshot(state.selectedProfile, state.choice, state.selectionRevision)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	payload := protocol.ContextSnapshotPayload{ContextMetricsPayload: state.contextSnapshot, Effort: effort}
	payload.Activity = state.contextActivity()
	event, err := protocol.NewContextSnapshot(state.loop.runtime.sessionID, state.loop.runtime.threadID, payload)
	state.publishContextQuery(request, event, err)
}

func (state *agentLoopRunState) contextActivity() string {
	if state.active != nil {
		if state.active.compactionID.Valid() {
			return "compacting"
		}
		return "turn"
	}
	if state.compaction != nil {
		return "compacting"
	}
	return "idle"
}

func (state *agentLoopRunState) publishContextQuery(request loopCommandRequest, event protocol.Event, err error) {
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
	admission, code := state.loop.runtime.preflightCompactionFor(state.selectedProfile, request.command.Instructions())
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
	state.compaction = &activeLoopCompaction{profile: state.selectedProfile, selectionRevision: state.selectionRevision, id: id, requestID: request.command.RequestID(), cancel: cancel, events: events, finished: finished}
	go func() {
		state.loop.runtime.cachePublisher = func(snapshot protocol.CacheSnapshotPayload) {
			events <- runtimePublication{cacheOnly: true, cacheSnapshot: snapshot}
		}
		err := state.loop.runtime.runCompaction(operationCtx, id, admission, func(event protocol.Event) { events <- state.loop.runtime.publishContext(event) })
		state.loop.runtime.cachePublisher = nil
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
	state.compaction.cancelRequested = true
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
	if publication.cacheOnly {
		state.cacheSnapshot = publication.cacheSnapshot
		return
	}
	item, err := state.correlateCompactionEvent(state.compaction.requestID, event)
	scope, scopeErr := protocol.CompactionEventScope(event)
	if err != nil || scopeErr != nil || scope != domain.ManualCompactionScope() || !state.compaction.acceptEvent(event) {
		state.runErr = fault.New(fault.CodeTurnFailed, "compaction emitted an invalid event")
		state.mode = loopModeClosing
		state.discardQueue(protocol.ControlErrorSessionFailed)
		state.cancelActive()
		return
	}
	if event.Kind == protocol.EventCompactionStarted {
		state.compaction.started = true
	}
	if event.Kind == protocol.EventCompactionCompleted || event.Kind == protocol.EventCompactionFailed {
		state.compaction.terminalSeen = true
		state.compaction.succeeded = event.Kind == protocol.EventCompactionCompleted
		state.contextSnapshot, state.contextSnapshotErr = publication.snapshot, publication.snapshotErr
		state.cacheSnapshot = publication.cacheSnapshot
	}
	state.loop.outputs <- item
}

func (state *agentLoopRunState) correlateCompactionEvent(requestID protocol.RequestID, event protocol.Event) (protocol.ControlItem, error) {
	if event.SessionID != state.loop.runtime.sessionID || event.ThreadID != state.loop.runtime.threadID {
		return protocol.ControlItem{}, fault.New(fault.CodeStreamProtocol, "compaction event belongs to another session or thread")
	}
	return protocol.NewCorrelatedContextItem(requestID, event)
}

func (operation *activeLoopCompaction) acceptEvent(event protocol.Event) bool {
	if event.CompactionID != operation.id || operation.terminalSeen {
		return false
	}
	switch event.Kind {
	case protocol.EventCompactionStarted:
		return !operation.started
	case protocol.EventCompactionFailed:
		failure, err := protocol.DecodeCompactionFailed(event)
		return err == nil && (operation.started || !failure.Durable)
	default:
		return operation.started
	}
}

// acceptCompactionEvent 仅用真实终态关闭自动压缩，保留后续采样的会话身份。
func (turn *activeLoopTurn) acceptCompactionEvent(event protocol.Event) error {
	scope, err := protocol.CompactionEventScope(event)
	if err != nil || scope.Trigger != domain.CompactionAuto || scope.TurnID != turn.turnID || turn.terminalSeen {
		return fault.New(fault.CodeStreamProtocol, "automatic compaction scope mismatch")
	}
	if !turn.compactionID.Valid() {
		if turn.autoCompactionSeen && scope.SampleIndex <= turn.compactionScope.SampleIndex {
			return fault.New(fault.CodeStreamProtocol, "automatic compaction event is stale")
		}
		switch event.Kind {
		case protocol.EventCompactionStarted:
			turn.compactionID = event.CompactionID
		case protocol.EventCompactionFailed:
			failure, err := protocol.DecodeCompactionFailed(event)
			if err != nil || failure.Durable || !preStartCompactionFailure(failure.Code) {
				return fault.New(fault.CodeStreamProtocol, "automatic compaction failed without a valid start")
			}
		default:
			return fault.New(fault.CodeStreamProtocol, "automatic compaction event has no matching start")
		}
		turn.autoCompactionSeen, turn.compactionScope = true, scope
		return nil
	}
	if event.CompactionID != turn.compactionID || scope != turn.compactionScope || event.Kind == protocol.EventCompactionStarted {
		return fault.New(fault.CodeStreamProtocol, "automatic compaction identity mismatch")
	}
	if event.Kind == protocol.EventCompactionCompleted || event.Kind == protocol.EventCompactionFailed {
		turn.compactionID = ""
	}
	return nil
}

func preStartCompactionFailure(code protocol.CompactionFailureCode) bool {
	return code == protocol.CompactionFailureCancelled || code == protocol.CompactionFailureTimeout || code == protocol.CompactionFailureSession
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
	state.refreshContextSnapshot()
}

// runtimePublication 将同一提交边界的事件与只读快照一起交给控制 owner。
type runtimePublication struct {
	cacheOnly     bool
	cacheSnapshot protocol.CacheSnapshotPayload
	event         protocol.Event
	snapshot      protocol.ContextMetricsPayload
	snapshotErr   error
}

func (runtime *Runtime) publishContext(event protocol.Event) runtimePublication {
	if event.Kind == protocol.EventTurnFailed {
		runtime.cache.snapshot.LastFailure = "turn_failed"
	}
	if event.Kind == protocol.EventCompactionFailed {
		runtime.cache.snapshot.LastFailure = "compaction_failed"
	}
	publication := runtimePublication{event: event, cacheSnapshot: runtime.cache.snapshot}
	if !protocol.IsTransientPresentation(event.Kind) {
		publication.snapshot, publication.snapshotErr = runtime.contextSnapshot()
	}
	return publication
}
