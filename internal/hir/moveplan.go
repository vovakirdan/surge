package hir

// MovePolicy determines how a value can be moved.
type MovePolicy uint8

const (
	// MoveUnknown indicates the move policy is not yet determined.
	MoveUnknown MovePolicy = iota

	// MoveCopy indicates the value is safe to copy.
	MoveCopy

	// MoveAllowed indicates the move is allowed if no active borrows.
	MoveAllowed

	// MoveForbidden indicates moving the value is prohibited.
	MoveForbidden

	// MoveNeedsDrop indicates the value must be explicitly dropped.
	MoveNeedsDrop
)

func (p MovePolicy) String() string {
	switch p {
	case MoveUnknown:
		return "MoveUnknown"
	case MoveCopy:
		return "MoveCopy"
	case MoveAllowed:
		return "MoveAllowed"
	case MoveForbidden:
		return "MoveForbidden"
	case MoveNeedsDrop:
		return "MoveNeedsDrop"
	default:
		return "MovePolicy(?)"
	}
}

// MoveInfo captures movement details for a value.
type MoveInfo struct {
	Policy MovePolicy
	Why    string // debug-friendly reason ("non-copy", "borrowed here", etc.)
}

// MovePlan maps values to their move policies.
type MovePlan struct {
	Func  FuncID
	Local map[LocalID]MoveInfo
}
