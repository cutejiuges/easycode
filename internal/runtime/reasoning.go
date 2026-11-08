package runtime

import (
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
)

// samplePresentationState 的生命周期与当前采样一致，绝不写入 Session。
type samplePresentationState struct {
	index        uint32
	stream       protocol.ReasoningStreamState
	resolution   protocol.SampleResolution
	presentation domain.SamplePresentation
}

func (r *Runtime) startSamplePresentation(index uint32) {
	r.samplePresentation = &samplePresentationState{index: index, resolution: protocol.NewUncommittedSampleResolution(index)}
}

func (r *Runtime) bindReasoning(event protocol.Event) (protocol.Event, error) {
	if r.samplePresentation == nil {
		return protocol.Event{}, fault.New(fault.CodeStreamProtocol, "reasoning has no active sample")
	}
	bound, err := protocol.BindReasoningSample(event, r.samplePresentation.index)
	if err != nil {
		return protocol.Event{}, err
	}
	payload, err := protocol.DecodeAssistantReasoning(bound)
	if err != nil {
		return protocol.Event{}, err
	}
	if !payload.Source.MatchesProvider(r.conversation.Family()) {
		return protocol.Event{}, fault.New(fault.CodeStreamProtocol, "reasoning source does not match provider")
	}
	if err := r.samplePresentation.stream.Accept(payload); err != nil {
		return protocol.Event{}, err
	}
	return bound, nil
}

func (r *Runtime) prepareSamplePresentation(sample *provider.PreparedSample) error {
	if r.samplePresentation == nil {
		return fault.New(fault.CodeStreamProtocol, "prepared sample has no presentation scope")
	}
	presentation, err := sample.Presentation()
	if err != nil {
		return err
	}
	// 最大合法 Sequence 在 admission 前预检编码预算，实际 seq 只能更短。
	event, err := protocol.NewSampleTranscriptCommitted(^uint64(0), r.samplePresentation.index, presentation)
	if err != nil {
		return err
	}
	payload, err := protocol.DecodeTranscriptCommitted(event)
	if err != nil {
		return err
	}
	state := r.samplePresentation.stream
	if err := state.Commit(*payload.Sample); err != nil {
		return err
	}
	r.samplePresentation.presentation = presentation
	return nil
}

func (r *Runtime) sampleWriteFailed() {
	if r.samplePresentation != nil && r.journal.Poisoned() {
		r.samplePresentation.resolution = protocol.NewUncertainSampleResolution(r.samplePresentation.index)
	}
}

func (r *Runtime) sampleDurable(sequence uint64) error {
	state := r.samplePresentation
	if state == nil {
		return fault.New(fault.CodeStreamProtocol, "durable sample has no presentation scope")
	}
	resolution, err := protocol.NewDurableSampleResolution(state.index, sequence, state.presentation)
	if err != nil {
		state.resolution = protocol.NewUncertainSampleResolution(state.index)
		return err
	}
	state.resolution = resolution
	return nil
}

func (r *Runtime) emitSampleCommit(turn domain.TurnID, sequence uint64) {
	state := r.samplePresentation
	if state == nil || r.turnEmitter == nil {
		return
	}
	event, err := protocol.NewSampleTranscriptCommitted(sequence, state.index, state.presentation)
	if err != nil {
		return
	}
	r.turnEmitter(r.decorate(event, turn))
	r.samplePresentation = nil
}
