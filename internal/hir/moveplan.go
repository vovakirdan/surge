package hir

type MovePolicy uint8

const (
	MoveUnknown MovePolicy = iota

	// Definitely safe, value is Copy in language semantics.
	MoveCopy

	// Move is allowed (non-Copy), but only if no active borrows.
	MoveAllowed

	// Move is forbidden at some points due to borrows / captures / thread escape.
	MoveForbidden

	// Value must be dropped/cleaned up at end (resource type).
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

type MoveInfo struct {
	Policy MovePolicy
	Why    string // debug-friendly reason ("non-copy", "borrowed here", etc.)
}

type MovePlan struct {
	Func  FuncID
	Local map[LocalID]MoveInfo
}
