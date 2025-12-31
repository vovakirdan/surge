package asyncrt

// WakerKind identifies a wait queue category.
type WakerKind uint8

const (
	WakerInvalid WakerKind = iota
	WakerJoin
)

// WakerKey identifies a wait queue entry.
type WakerKey struct {
	Kind WakerKind
	A    uint64
	B    uint64
}

// IsValid reports whether the key is usable for waiting.
func (k WakerKey) IsValid() bool {
	return k.Kind != WakerInvalid
}

// JoinKey builds a join wait key for a target task.
func JoinKey(target TaskID) WakerKey {
	return WakerKey{Kind: WakerJoin, A: uint64(target)}
}

// PollOutcomeKind reports how a poll iteration completed.
type PollOutcomeKind uint8

const (
	PollDone PollOutcomeKind = iota
	PollYielded
	PollParked
)

// PollOutcome describes the outcome of polling a task once.
type PollOutcome struct {
	Kind    PollOutcomeKind
	Value   any
	ParkKey WakerKey
}
