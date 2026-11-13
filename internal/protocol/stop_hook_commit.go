package protocol

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"fmt"
)

// StopHookPhase 是已持久化的完成检查边界，不把未交付意图当作原生消息。
type StopHookPhase string

const (
	StopHookPlanned   StopHookPhase = "planned"
	StopHookStarted   StopHookPhase = "started"
	StopHookFinished  StopHookPhase = "finished"
	StopHookDecided   StopHookPhase = "decided"
	StopHookDelivered StopHookPhase = "delivered"
)

func (p StopHookPhase) Valid() bool {
	switch p {
	case StopHookPlanned, StopHookStarted, StopHookFinished, StopHookDecided, StopHookDelivered:
		return true
	}
	return false
}

type StopHookCommittedPayload struct {
	Target         domain.HookTarget      `json:"target"`
	PlanDigest     string                 `json:"plan_digest"`
	DetailSequence uint64                 `json:"detail_sequence"`
	Phase          StopHookPhase          `json:"phase"`
	Disposition    domain.StopDisposition `json:"disposition,omitempty"`
}

func (p StopHookCommittedPayload) Validate() error {
	if !p.Target.ValidFor(domain.HookStop) || !domain.ValidHookDigest(p.PlanDigest) || p.DetailSequence == 0 || !p.Phase.Valid() {
		return fmt.Errorf("invalid stop hook commit")
	}
	if p.Phase == StopHookDecided || p.Phase == StopHookDelivered {
		switch p.Disposition {
		case domain.StopPassed, domain.StopBlocked, domain.StopContinued, domain.StopCancelled, domain.StopError, domain.StopUncertain:
		default:
			return fmt.Errorf("invalid stop disposition")
		}
		if p.Phase == StopHookDelivered && p.Disposition != domain.StopContinued {
			return fmt.Errorf("invalid stop delivery")
		}
	} else if p.Disposition != "" {
		return fmt.Errorf("unexpected stop disposition")
	}
	return nil
}
func NewStopHookCommitted(p StopHookCommittedPayload) (Event, error) {
	if err := p.Validate(); err != nil {
		return Event{}, err
	}
	return newEventWithPayload(EventStopHookCommitted, p)
}
func DecodeStopHookCommitted(e Event) (StopHookCommittedPayload, error) {
	var p StopHookCommittedPayload
	if e.Kind != EventStopHookCommitted || len(e.Payload) > 2048 || codec.UnmarshalRecord(e.Payload, &p) != nil {
		return p, fmt.Errorf("invalid stop hook commit")
	}
	sample, _ := p.Target.AssistantSample()
	if e.TurnID != "" && sample.TurnID != e.TurnID {
		return p, fmt.Errorf("stop commit scope mismatch")
	}
	return p, p.Validate()
}
