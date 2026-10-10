package session

// Cardinality 描述当前 kind 在一个 thread 中的出现次数约束。
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

// Descriptor 是一种已知 event kind 的唯一当前语义声明。
type Descriptor struct {
	Kind        EventKind
	Cardinality Cardinality
	Placement   Placement
}

func descriptorByKind(kind EventKind) (Descriptor, bool) {
	switch kind {
	case EventSessionMeta:
		return Descriptor{
			Kind:        EventSessionMeta,
			Cardinality: CardinalityExactlyOne, Placement: PlacementInitialMetadata,
		}, true
	case EventThreadMeta:
		return Descriptor{
			Kind:        EventThreadMeta,
			Cardinality: CardinalityExactlyOne, Placement: PlacementInitialMetadata,
		}, true
	case EventTurnStarted:
		return Descriptor{
			Kind:        EventTurnStarted,
			Cardinality: CardinalityMany, Placement: PlacementTurnStart,
		}, true
	case EventProviderNativeCommit:
		return Descriptor{
			Kind:        EventProviderNativeCommit,
			Cardinality: CardinalityMany, Placement: PlacementActiveTurn,
		}, true
	case EventSampleUsage:
		return Descriptor{
			Kind:        EventSampleUsage,
			Cardinality: CardinalityMany, Placement: PlacementActiveTurn,
		}, true
	case EventToolCallReady:
		return Descriptor{
			Kind:        EventToolCallReady,
			Cardinality: CardinalityMany, Placement: PlacementActiveTurn,
		}, true
	case EventToolExecutionStarted:
		return Descriptor{
			Kind:        EventToolExecutionStarted,
			Cardinality: CardinalityMany, Placement: PlacementActiveTurn,
		}, true
	case EventToolCallResult:
		return Descriptor{
			Kind:        EventToolCallResult,
			Cardinality: CardinalityMany, Placement: PlacementActiveTurn,
		}, true
	case EventTurnCompleted:
		return Descriptor{
			Kind:        EventTurnCompleted,
			Cardinality: CardinalityMany, Placement: PlacementTurnTerminal,
		}, true
	case EventTurnFailed:
		return Descriptor{
			Kind:        EventTurnFailed,
			Cardinality: CardinalityMany, Placement: PlacementTurnTerminal,
		}, true
	default:
		return Descriptor{}, false
	}
}
