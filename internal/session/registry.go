package session

import (
	"fmt"
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
}

// LookupDescriptor 返回当前程序理解的 kind/revision 描述。
func LookupDescriptor(kind EventKind, version int) (Descriptor, bool) {
	descriptor, exists := descriptorByKind(kind)
	return descriptor, exists && descriptor.Version == version
}

func descriptorByKind(kind EventKind) (Descriptor, bool) {
	switch kind {
	case EventSessionMeta:
		return Descriptor{
			Kind: EventSessionMeta, Version: 1, Requirement: ReplayRequired,
			Cardinality: CardinalityExactlyOne, Placement: PlacementInitialMetadata,
		}, true
	case EventThreadMeta:
		return Descriptor{
			Kind: EventThreadMeta, Version: 1, Requirement: ReplayRequired,
			Cardinality: CardinalityExactlyOne, Placement: PlacementInitialMetadata,
		}, true
	case EventTurnStarted:
		return Descriptor{
			Kind: EventTurnStarted, Version: 1, Requirement: ReplayRequired,
			Cardinality: CardinalityMany, Placement: PlacementTurnStart,
		}, true
	case EventProviderNativeCommit:
		return Descriptor{
			Kind: EventProviderNativeCommit, Version: 1, Requirement: ReplayRequired,
			Cardinality: CardinalityMany, Placement: PlacementActiveTurn,
		}, true
	case EventTurnCompleted:
		return Descriptor{
			Kind: EventTurnCompleted, Version: 1, Requirement: ReplayRequired,
			Cardinality: CardinalityMany, Placement: PlacementTurnTerminal,
		}, true
	case EventTurnFailed:
		return Descriptor{
			Kind: EventTurnFailed, Version: 1, Requirement: ReplayRequired,
			Cardinality: CardinalityMany, Placement: PlacementTurnTerminal,
		}, true
	default:
		return Descriptor{}, false
	}
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
