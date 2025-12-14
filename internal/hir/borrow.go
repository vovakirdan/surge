package hir

import "surge/internal/source"

type BorrowKind uint8

const (
	BorrowShared BorrowKind = iota // &T
	BorrowMut                      // &mut T
)

func (k BorrowKind) String() string {
	switch k {
	case BorrowShared:
		return "&"
	case BorrowMut:
		return "&mut"
	default:
		return "?"
	}
}

type AccessKind uint8

const (
	AccessRead AccessKind = iota
	AccessWrite
	AccessMove
)

type LoanID int32
type EventID int32

type ScopeID uint32

const NoScopeID ScopeID = 0

// BorrowEdge is a borrow "edge": local From borrows from local To (or a projection thereof).
type BorrowEdge struct {
	From LocalID // borrower local (the reference value)
	To   LocalID // owner/borrowed-from local
	Kind BorrowKind
	Span source.Span
	// Scope is a lexical scope endpoint (best-effort v1). May be NoScopeID.
	Scope ScopeID
}

type BorrowEventKind uint8

const (
	EvBorrowStart BorrowEventKind = iota
	EvBorrowEnd
	EvMove
	EvWrite
	EvRead
	EvDrop // explicit @drop or implicit end-of-scope drop point
	EvSpawnEscape
)

func (k BorrowEventKind) String() string {
	switch k {
	case EvBorrowStart:
		return "borrow_start"
	case EvBorrowEnd:
		return "borrow_end"
	case EvMove:
		return "move"
	case EvWrite:
		return "write"
	case EvRead:
		return "read"
	case EvDrop:
		return "drop"
	case EvSpawnEscape:
		return "spawn_escape"
	default:
		return "unknown"
	}
}

type BorrowEvent struct {
	ID    EventID
	Kind  BorrowEventKind
	Local LocalID // main local
	Peer  LocalID // borrower/owner peer for borrow events; NoLocalID if not applicable
	Span  source.Span
	Scope ScopeID
	Note  string // optional, for debug dump
}

type BorrowGraph struct {
	Func   FuncID
	Edges  []BorrowEdge
	Events []BorrowEvent

	OutEdges map[LocalID][]int // indices into Edges
	InEdges  map[LocalID][]int
}
