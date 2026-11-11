package protocol

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"fmt"
)

// PostHookPhase 是已确定持久化的后置边界，供宿主刷新详情。
type PostHookPhase string

const (
	PostHookPlanned   PostHookPhase = "planned"
	PostHookStarted   PostHookPhase = "started"
	PostHookFinished  PostHookPhase = "finished"
	PostHookCompleted PostHookPhase = "completed"
)

func (p PostHookPhase) Valid() bool {
	return p == PostHookPlanned || p == PostHookStarted || p == PostHookFinished || p == PostHookCompleted
}

type PostHookCommittedPayload struct {
	InvocationID   string        `json:"invocation_id"`
	PlanDigest     string        `json:"plan_digest"`
	DetailSequence uint64        `json:"detail_sequence"`
	Phase          PostHookPhase `json:"phase"`
}

func (p PostHookCommittedPayload) Validate() error {
	if !validUUIDv7Text(p.InvocationID) || !domain.ValidHookDigest(p.PlanDigest) || p.DetailSequence == 0 || !p.Phase.Valid() {
		return fmt.Errorf("invalid post hook commit")
	}
	return nil
}
func NewPostHookCommitted(p PostHookCommittedPayload) (Event, error) {
	if err := p.Validate(); err != nil {
		return Event{}, err
	}
	return newEventWithPayload(EventPostHookCommitted, p)
}
func DecodePostHookCommitted(e Event) (PostHookCommittedPayload, error) {
	var p PostHookCommittedPayload
	if e.Kind != EventPostHookCommitted || len(e.Payload) > 1024 || codec.UnmarshalRecord(e.Payload, &p) != nil {
		return p, fmt.Errorf("invalid post hook commit")
	}
	return p, p.Validate()
}
