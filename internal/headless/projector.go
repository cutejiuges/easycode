package headless

import (
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

type projection struct {
	event     Event
	text      string
	terminal  bool
	completed bool
	failure   fault.Summary
}

type projector struct {
	sessionID domain.SessionID
	threadID  domain.ThreadID
	turnID    domain.TurnID
	started   bool
	terminal  bool
	sample    uint32
	reasoning protocol.ReasoningStreamState
}

func newProjector(sessionID domain.SessionID, threadID domain.ThreadID) *projector {
	return &projector{sessionID: sessionID, threadID: threadID}
}

func (projector *projector) project(event protocol.Event) (projection, error) {
	if projector.terminal {
		return projection{}, fmt.Errorf("runtime event followed terminal")
	}
	if event.SessionID != projector.sessionID || event.ThreadID != projector.threadID ||
		!event.SessionID.Valid() || !event.ThreadID.Valid() {
		return projection{}, fmt.Errorf("runtime event thread identity is invalid")
	}
	if event.ItemID != "" || event.CallID != "" {
		return projection{}, fmt.Errorf("runtime event contains unsupported identity")
	}

	switch event.Kind {
	case protocol.EventTurnStarted:
		if projector.started || !event.TurnID.Valid() {
			return projection{}, fmt.Errorf("runtime turn start is invalid")
		}
		if err := protocol.ValidateTurnStarted(event); err != nil {
			return projection{}, err
		}
		projector.started = true
		projector.turnID = event.TurnID
		external, err := NewTurnStartedEvent(event.SessionID, event.ThreadID, event.TurnID)
		return projection{event: external}, err
	case protocol.EventToolReady, protocol.EventToolStarted, protocol.EventToolProgress, protocol.EventToolTerminal:
		if err := projector.validateActiveTurn(event); err != nil {
			return projection{}, err
		}
		_, err := protocol.DecodeToolActivity(event)
		return projection{}, err
	case protocol.EventStopHookCommitted:
		if err := projector.validateActiveTurn(event); err != nil {
			return projection{}, err
		}
		_, err := protocol.DecodeStopHookCommitted(event)
		return projection{}, err
	case protocol.EventPostHookCommitted:
		if err := projector.validateActiveTurn(event); err != nil {
			return projection{}, err
		}
		_, err := protocol.DecodePostHookCommitted(event)
		return projection{}, err
	case protocol.EventTranscriptCommitted:
		if err := projector.validateActiveTurn(event); err != nil {
			return projection{}, err
		}
		payload, err := protocol.DecodeTranscriptCommitted(event)
		if err == nil && payload.Origin == protocol.TranscriptCommitSample {
			if payload.Sample.SampleIndex != projector.sample {
				return projection{}, fmt.Errorf("runtime sample commit index is invalid")
			}
			err = projector.reasoning.Commit(*payload.Sample)
			if err == nil {
				projector.sample++
			}
		}
		return projection{}, err
	case protocol.EventUserInputCommitted:
		if err := projector.validateActiveTurn(event); err != nil {
			return projection{}, err
		}
		_, err := protocol.DecodeUserInputCommitted(event)
		return projection{}, err
	case protocol.EventAssistantReasoning:
		if err := projector.validateActiveTurn(event); err != nil {
			return projection{}, err
		}
		payload, err := protocol.DecodeAssistantReasoning(event)
		if err != nil {
			return projection{}, err
		}
		if payload.SampleIndex != projector.sample {
			return projection{}, fmt.Errorf("runtime reasoning sample index is invalid")
		}
		return projection{}, projector.reasoning.Accept(payload)
	case protocol.EventAssistantTextDelta:
		if err := projector.validateActiveTurn(event); err != nil {
			return projection{}, err
		}
		payload, err := protocol.DecodeAssistantTextDelta(event)
		if err != nil {
			return projection{}, err
		}
		external, err := NewAssistantTextDeltaEvent(event.SessionID, event.ThreadID, event.TurnID, payload.Text)
		return projection{event: external, text: payload.Text}, err
	case protocol.EventTurnCompleted:
		if err := projector.validateActiveTurn(event); err != nil {
			return projection{}, err
		}
		if projector.reasoning.Pending() {
			return projection{}, fmt.Errorf("runtime reasoning sample is unresolved")
		}
		if err := projector.reasoning.Complete(); err != nil {
			return projection{}, err
		}
		payload, err := protocol.DecodeTurnCompleted(event)
		if err != nil {
			return projection{}, err
		}
		usage, err := payload.Usage.Domain()
		if err != nil {
			return projection{}, err
		}
		projector.terminal = true
		external, err := NewTurnCompletedEvent(event.SessionID, event.ThreadID, event.TurnID, usage)
		return projection{event: external, terminal: true, completed: true}, err
	case protocol.EventTurnFailed:
		if err := projector.validateActiveTurn(event); err != nil {
			return projection{}, err
		}
		payload, err := protocol.DecodeTurnFailed(event)
		if err != nil {
			return projection{}, err
		}
		if payload.Resolution != nil {
			if payload.Resolution.SampleIndex != projector.sample {
				return projection{}, fmt.Errorf("runtime failure sample index is invalid")
			}
			if err := projector.reasoning.Resolve(*payload.Resolution); err != nil {
				return projection{}, err
			}
		} else if projector.reasoning.Pending() {
			return projection{}, fmt.Errorf("runtime reasoning failure resolution is missing")
		} else if err := projector.reasoning.Complete(); err != nil {
			return projection{}, err
		}
		failure := fault.Summary{
			Code: fault.Code(payload.Code), Message: payload.Message, Cancelled: payload.Cancelled,
		}
		external, err := NewTurnFailedEvent(event.SessionID, event.ThreadID, event.TurnID, failure)
		if err != nil {
			return projection{}, err
		}
		projector.terminal = true
		return projection{event: external, terminal: true, failure: failure}, nil
	default:
		return projection{}, fmt.Errorf("runtime event kind is unsupported")
	}
}

func (projector *projector) validateActiveTurn(event protocol.Event) error {
	if !projector.started || !projector.turnID.Valid() || event.TurnID != projector.turnID {
		return fmt.Errorf("runtime event turn identity is invalid")
	}
	return nil
}
