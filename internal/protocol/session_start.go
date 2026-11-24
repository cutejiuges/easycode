package protocol

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// SessionStartState 区分真实计划、交付与未交付的宿主状态。
type SessionStartState string

const (
	SessionStartPlanned   SessionStartState = "planned"
	SessionStartApplied   SessionStartState = "applied"
	SessionStartSkipped   SessionStartState = "skipped"
	SessionStartCancelled SessionStartState = "cancelled"
	SessionStartUncertain SessionStartState = "outcome_uncertain"
)

// SessionStartPayload 只携带阶段与位置，模型正文由宿主详情查询读取。
type SessionStartPayload struct {
	Target     domain.SessionStartTarget `json:"target"`
	PlanDigest string                    `json:"plan_digest"`
	State      SessionStartState         `json:"state"`
	Code       domain.HookCode           `json:"code"`
	Count      int                       `json:"count"`
	Sequence   uint64                    `json:"sequence"`
	Revision   uint64                    `json:"revision"`
}

func (payload SessionStartPayload) Validate() error {
	valid := payload.Target.Validate() == nil && payload.Count >= 0 && payload.Count <= domain.MaxHookMatches && payload.Code.Valid()
	hasPlan := domain.ValidHookDigest(payload.PlanDigest)
	if !valid || payload.PlanDigest != "" && !hasPlan {
		return fmt.Errorf("invalid session start scope")
	}
	valid = false
	switch payload.State {
	case SessionStartPlanned:
		valid = hasPlan && payload.Count > 0 && payload.Code == domain.HookOK && payload.Sequence == 0 && payload.Revision == 0
	case SessionStartApplied:
		valid = hasPlan && payload.Count > 0 && (payload.Code == domain.HookOK || payload.Code == domain.HookExecutionBudgetExhausted) && payload.Sequence > payload.Target.CheckpointSequence && payload.Revision > payload.Target.CheckpointRevision
	case SessionStartSkipped:
		valid = payload.Revision == 0 && (hasPlan && payload.Sequence > payload.Target.CheckpointSequence || !hasPlan && payload.Sequence == 0 && payload.Count == 0 && (payload.Code == domain.HookConfigInvalid || payload.Code == domain.HookMatchLimit))
	case SessionStartCancelled:
		valid = hasPlan && payload.Code == domain.HookCancelled && payload.Sequence > payload.Target.CheckpointSequence && payload.Revision == 0
	case SessionStartUncertain:
		valid = hasPlan && payload.Code == domain.HookOutcomeUncertain && payload.Revision == 0 && (payload.Sequence == 0 || payload.Sequence > payload.Target.CheckpointSequence)
	}
	if !valid {
		return fmt.Errorf("invalid session start state")
	}
	return nil
}

func NewSessionStart(session domain.SessionID, thread domain.ThreadID, payload SessionStartPayload) (Event, error) {
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	event, err := newEventWithPayload(EventSessionStart, payload)
	if err != nil {
		return Event{}, err
	}
	event.SessionID, event.ThreadID = session, thread
	event.CompactionID, event.TurnID = payload.Target.CompactionID, sessionStartTurn(payload.Target)
	return event, event.Validate()
}

func sessionStartTurn(target domain.SessionStartTarget) domain.TurnID {
	if target.Source == domain.SessionStartCompact {
		return target.Scope.TurnID
	}
	return target.TurnID
}

func DecodeSessionStart(event Event) (SessionStartPayload, error) {
	var payload SessionStartPayload
	if event.Kind != EventSessionStart || !event.SessionID.Valid() || !event.ThreadID.Valid() || len(event.Payload) > 2048 ||
		codec.DecodeObject(event.Payload, &payload, "target", "plan_digest", "state", "code", "count", "sequence", "revision") != nil ||
		event.CompactionID != payload.Target.CompactionID || event.TurnID != sessionStartTurn(payload.Target) {
		return payload, fmt.Errorf("invalid session start event")
	}
	return payload, payload.Validate()
}
