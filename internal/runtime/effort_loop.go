package runtime

import (
	"errors"
	"math"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
)

// effortSnapshot 只读取控制 owner 已拥有的值，不访问活动 Conversation。
func (state *agentLoopRunState) effortSnapshot(profile contextplan.ProviderProfile, choice domain.EffortChoice, revision uint64) (protocol.EffortSnapshotPayload, error) {
	startup, err := protocol.NewEffortValuePayload(state.loop.runtime.profile.InferenceSettings())
	if err != nil {
		return protocol.EffortSnapshotPayload{}, err
	}
	selected, err := protocol.NewEffortValuePayload(profile.InferenceSettings())
	if err != nil {
		return protocol.EffortSnapshotPayload{}, err
	}
	levels, err := contextplan.AllowedEffortLevels(state.loop.runtime.profile.InferenceSettings(), state.loop.runtime.budget)
	if err != nil {
		return protocol.EffortSnapshotPayload{}, err
	}
	source := "startup"
	switch choice.Mode() {
	case domain.EffortAuto:
		source = "auto"
	case domain.EffortValue:
		source = "runtime"
	}
	payload := protocol.EffortSnapshotPayload{Scope: "process", Family: profile.Family(), Startup: startup, Selected: selected, Source: source, SelectionRevision: revision, Activity: "idle", AllowedLevels: levels}
	var active protocol.EffortActivePayload
	if state.active != nil {
		captured, captureErr := protocol.NewEffortValuePayload(state.active.profile.InferenceSettings())
		if captureErr != nil {
			return payload, captureErr
		}
		phase := "starting"
		if state.active.turnID.Valid() {
			phase = "running"
		}
		if state.active.terminalSeen {
			phase = "finishing"
		}
		if state.active.turnID.Valid() {
			active, err = protocol.NewTurnEffortActive(state.active.turnID, phase, captured, state.active.selectionRevision, state.active.worker.cancelRequested)
		} else {
			active, err = protocol.NewStartingEffortActive(state.active.input.requestID, phase, captured, state.active.selectionRevision, state.active.worker.cancelRequested)
		}
		payload.Activity = "turn"
		if state.active.compactionID.Valid() {
			payload.Activity = "compacting"
		}
		payload.Active = &active
	} else if state.compaction != nil {
		captured, captureErr := protocol.NewEffortValuePayload(state.compaction.profile.InferenceSettings())
		if captureErr != nil {
			return payload, captureErr
		}
		phase := "starting"
		if state.compaction.started {
			phase = "running"
		}
		if state.compaction.terminalSeen {
			phase = "finishing"
		}
		active, err = protocol.NewCompactionEffortActive(state.compaction.id, phase, captured, state.compaction.selectionRevision, state.compaction.worker.cancelRequested)
		payload.Activity = "compacting"
		payload.Active = &active
	}
	if err != nil {
		return payload, err
	}
	payload.Pending = payload.Active != nil && payload.Active.Effort() != selected
	return payload, payload.Validate()
}

func (state *agentLoopRunState) controlEffort(request loopCommandRequest) {
	if request.command.Kind() == protocol.CommandSetEffort && (state.loop.runtime.poisoned.Load() || state.loop.runtime.journal.Poisoned()) {
		state.reject(request, protocol.ControlErrorSessionFailed, true)
		return
	}
	profile, choice, revision := state.selectedProfile, state.choice, state.selectionRevision
	disposition := protocol.CommandDispositionQueried
	if request.command.Kind() == protocol.CommandSetEffort {
		choice = request.command.EffortChoice()
		settings, err := choice.Apply(state.loop.runtime.profile.InferenceSettings())
		if err != nil {
			code := protocol.ControlErrorEffortConflict
			var invalid *domain.EffortError
			if errors.As(err, &invalid) && invalid.Kind() == domain.EffortInvalid {
				code = protocol.ControlErrorInvalidEffort
			}
			state.reject(request, code, true)
			return
		}
		if err := state.loop.runtime.budget.ValidateInference(settings); err != nil {
			state.reject(request, protocol.ControlErrorEffortConflict, true)
			return
		}
		profile, err = profile.WithInferenceSettings(settings)
		if err != nil {
			state.reject(request, protocol.ControlErrorEffortConflict, true)
			return
		}
		if choice != state.choice {
			if revision == math.MaxUint64 {
				state.reject(request, protocol.ControlErrorInvalidCommand, true)
				return
			}
			revision++
		}
		disposition = protocol.CommandDispositionUpdated
	}
	payload, err := state.effortSnapshot(profile, choice, revision)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	event, err := state.loop.effortSnapshot(state.loop.runtime.sessionID, state.loop.runtime.threadID, payload)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	item, err := protocol.NewCorrelatedEffortItem(request.command.RequestID(), event)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	result, err := protocol.NewAcceptedCommandResult(request.command, disposition)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	if _, err = protocol.NewCommandResultItem(result); err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	// 所有可失败的设置和编码已完成；观测失败不得撤回该内存交换。
	state.choice, state.selectedProfile, state.selectionRevision = choice, profile, revision
	if state.active == nil && state.compaction == nil {
		state.refreshContextSnapshot()
	}
	state.emitResult(request, result)
	state.loop.outputs <- item
	state.completeRequest(request.command.RequestID())
}
