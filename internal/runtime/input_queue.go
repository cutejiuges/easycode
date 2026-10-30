package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
)

type inputClaimRequest struct {
	turnID  domain.TurnID
	index   uint32
	control *loopInputControl
}
type inputResolution struct {
	turnID  domain.TurnID
	inputs  []domain.UserInput
	state   protocol.InputState
	reason  protocol.InputReason
	control *loopInputControl
}

// loopInputControl 的固定通道由一个 turn worker 持有，owner 在同一事件循环处理握手。
type loopInputControl struct {
	loop     *AgentLoop
	reply    chan []domain.UserInput
	resolved chan struct{}
}

func (control *loopInputControl) claim(ctx context.Context, turnID domain.TurnID, index uint32) ([]domain.UserInput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case control.loop.claims <- inputClaimRequest{turnID: turnID, index: index, control: control}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return <-control.reply, nil
}
func (control *loopInputControl) resolve(turnID domain.TurnID, inputs []domain.UserInput, state protocol.InputState, reason protocol.InputReason) {
	control.loop.resolutions <- inputResolution{turnID: turnID, inputs: inputs, state: state, reason: reason, control: control}
	<-control.resolved
}

func queuedDomainInputs(inputs []queuedInput) []domain.UserInput {
	values := make([]domain.UserInput, len(inputs))
	for i, input := range inputs {
		values[i] = input.value
	}
	return values
}

func (state *agentLoopRunState) emitInputStatus(input queuedInput, turnID domain.TurnID, status protocol.InputState, reason protocol.InputReason) {
	if !input.automatic {
		return
	}
	value, err := protocol.NewInputStatus(input.requestID, input.value.ID(), status, turnID, reason)
	if err != nil {
		return
	}
	item, err := protocol.NewInputStatusItem(value)
	if err == nil {
		state.loop.outputs <- item
	}
}

func (state *agentLoopRunState) takePrefix(turnID domain.TurnID, index uint32, initial bool) []queuedInput {
	count := 0
	for i, input := range state.queue {
		if !input.automatic && (!initial || i > 0) {
			break
		}
		inputs := queuedDomainInputs(state.queue[:i+1])
		a, err := inputAssociation(turnID, index, inputs)
		if err != nil || !state.loop.runtime.inputEncodingFits(a, inputs) {
			break
		}
		count = i + 1
		if !input.automatic {
			break
		}
	}
	if count == 0 {
		return nil
	}
	taken := append([]queuedInput(nil), state.queue[:count]...)
	copy(state.queue, state.queue[count:])
	clear(state.queue[len(state.queue)-count:])
	state.queue = state.queue[:len(state.queue)-count]
	return taken
}

func (state *agentLoopRunState) handleInputClaim(request inputClaimRequest) {
	var claimed []queuedInput
	if state.active != nil && state.active.turnID == request.turnID && !state.active.cancelRequested && len(state.active.claim) == 0 && !state.active.terminalSeen {
		claimed = state.takePrefix(request.turnID, request.index, false)
		state.active.claim = claimed
		for _, input := range claimed {
			state.emitInputStatus(input, request.turnID, protocol.InputClaimed, "")
		}
	}
	request.control.reply <- queuedDomainInputs(claimed)
}

func (state *agentLoopRunState) handleInputResolution(resolution inputResolution) {
	if state.active != nil && state.active.turnID == resolution.turnID {
		claim := state.active.claim
		matches := len(claim) == len(resolution.inputs)
		for i := range claim {
			if !matches {
				break
			}
			matches = claim[i].value.ID() == resolution.inputs[i].ID()
		}
		if matches {
			for _, input := range claim {
				state.queuedBytes -= len(input.text)
				state.emitInputStatus(input, resolution.turnID, resolution.state, resolution.reason)
				if input.requestID != state.active.input.requestID {
					state.completeRequest(input.requestID)
				}
			}
			state.active.claim = nil
		} else {
			state.mode = loopModeClosing
			state.cancelActive()
		}
	}
	resolution.control.resolved <- struct{}{}
}

func (state *agentLoopRunState) returnAutomatic(reason protocol.InputReason) {
	kept := state.queue[:0]
	for _, input := range state.queue {
		if input.automatic {
			state.queuedBytes -= len(input.text)
			state.emitInputStatus(input, "", protocol.InputReturned, reason)
			state.completeRequest(input.requestID)
		} else {
			kept = append(kept, input)
		}
	}
	clear(state.queue[len(kept):])
	state.queue = kept
}

func (state *agentLoopRunState) retract(request loopCommandRequest) {
	ids := request.command.InputIDs()
	targets := make(map[domain.InputID]bool, len(ids))
	for _, id := range ids {
		targets[id] = false
	}
	for _, input := range state.queue {
		if _, ok := targets[input.value.ID()]; ok && input.automatic {
			targets[input.value.ID()] = true
		}
	}
	for _, found := range targets {
		if !found {
			state.reject(request, protocol.ControlErrorInputNotQueued, true)
			return
		}
	}
	result, _ := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionRetracted)
	state.emitResult(request, result)
	state.completeRequest(request.command.RequestID())
	kept := state.queue[:0]
	for _, input := range state.queue {
		if targets[input.value.ID()] {
			state.queuedBytes -= len(input.text)
			state.emitInputStatus(input, "", protocol.InputReturned, protocol.InputReasonRetracted)
			state.completeRequest(input.requestID)
		} else {
			kept = append(kept, input)
		}
	}
	clear(state.queue[len(kept):])
	state.queue = kept
}
