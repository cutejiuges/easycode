package session

import (
	"fmt"
	"reflect"
)

// Cardinality 描述当前 revision 在一个 thread 中的出现次数约束。
type Cardinality string

const (
	CardinalityExactlyOne Cardinality = "exactly_one"
	CardinalityMany       Cardinality = "many"
)

// Placement 描述记录在 Session lifecycle 中允许出现的位置。
type Placement string

const (
	PlacementInitialMetadata Placement = "initial_metadata"
	PlacementTurnStart       Placement = "turn_start"
	PlacementActiveTurn      Placement = "active_turn"
	PlacementTurnTerminal    Placement = "turn_terminal"
)

// Descriptor 是一种已知 payload revision 的唯一语义声明。
type Descriptor struct {
	Kind        EventKind
	Version     int
	Requirement ReplayRequirement
	Cardinality Cardinality
	Placement   Placement
	payloadType reflect.Type
}

var descriptors = map[EventKind]Descriptor{
	EventSessionMeta: {
		Kind: EventSessionMeta, Version: 1, Requirement: ReplayRequired,
		Cardinality: CardinalityExactlyOne, Placement: PlacementInitialMetadata,
		payloadType: reflect.TypeFor[SessionMetaPayload](),
	},
	EventThreadMeta: {
		Kind: EventThreadMeta, Version: 1, Requirement: ReplayRequired,
		Cardinality: CardinalityExactlyOne, Placement: PlacementInitialMetadata,
		payloadType: reflect.TypeFor[ThreadMetaPayload](),
	},
	EventTurnStarted: {
		Kind: EventTurnStarted, Version: 1, Requirement: ReplayRequired,
		Cardinality: CardinalityMany, Placement: PlacementTurnStart,
		payloadType: reflect.TypeFor[TurnStartedPayload](),
	},
	EventProviderNativeCommit: {
		Kind: EventProviderNativeCommit, Version: 1, Requirement: ReplayRequired,
		Cardinality: CardinalityMany, Placement: PlacementActiveTurn,
		payloadType: reflect.TypeFor[NativeCommitPayload](),
	},
	EventTurnCompleted: {
		Kind: EventTurnCompleted, Version: 1, Requirement: ReplayRequired,
		Cardinality: CardinalityMany, Placement: PlacementTurnTerminal,
		payloadType: reflect.TypeFor[TurnCompletedPayload](),
	},
	EventTurnFailed: {
		Kind: EventTurnFailed, Version: 1, Requirement: ReplayRequired,
		Cardinality: CardinalityMany, Placement: PlacementTurnTerminal,
		payloadType: reflect.TypeFor[TurnFailedPayload](),
	},
}

// LookupDescriptor 返回当前程序理解的 kind/revision 描述。
func LookupDescriptor(kind EventKind, version int) (Descriptor, bool) {
	descriptor, exists := descriptors[kind]
	return descriptor, exists && descriptor.Version == version
}

func descriptorForDraft(draft RecordDraft) (Descriptor, error) {
	descriptor, exists := descriptors[draft.EventKind]
	if !exists {
		return Descriptor{}, fmt.Errorf("unsupported session event kind")
	}
	if draft.Payload == nil || reflect.TypeOf(draft.Payload) != descriptor.payloadType {
		return Descriptor{}, fmt.Errorf("session payload type does not match event kind")
	}
	return descriptor, nil
}

func validateKnownDeclaration(record Record) error {
	descriptor, exact := LookupDescriptor(record.EventKind, record.PayloadVersion)
	if !exact {
		return nil
	}
	if descriptor.Requirement != record.ReplayRequirement {
		return fmt.Errorf("session replay requirement does not match registry")
	}
	return nil
}
