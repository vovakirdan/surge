package hir

import "surge/internal/source"

// BorrowKind distinguishes between shared and mutable borrows.
type BorrowKind uint8

const (
	// BorrowShared represents an immutable borrow (&T).
	BorrowShared BorrowKind = iota // &T
	// BorrowMut represents a mutable borrow (&mut T).
	BorrowMut // &mut T
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

// AccessKind identifies the type of access to a variable.
type AccessKind uint8

const (
	// AccessRead represents a read access.
	AccessRead AccessKind = iota
	// AccessWrite represents a write access.
	AccessWrite
	AccessMove
)

// LoanID identifies a unique loan.
type LoanID int32

// EventID identifies a borrow event.
type EventID int32

// ScopeID identifies a lexical scope.
type ScopeID uint32

// NoScopeID indicates no scope.
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

// BorrowEventKind identifies the type of borrow event.
type BorrowEventKind uint8

const (
	// EvBorrowStart indicates the beginning of a borrow.
	EvBorrowStart BorrowEventKind = iota
	// EvBorrowEnd indicates the end of a borrow.
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

// BorrowEvent represents an event in the borrow checker.
type BorrowEvent struct {
	ID    EventID
	Kind  BorrowEventKind
	Local LocalID // main local
	Peer  LocalID // borrower/owner peer for borrow events; NoLocalID if not applicable
	Span  source.Span
	Scope ScopeID
	Note  string // optional, for debug dump
}

// BorrowGraph represents the borrow relationships in a function.
type BorrowGraph struct {
	Func   FuncID
	Edges  []BorrowEdge
	Events []BorrowEvent

	OutEdges map[LocalID][]int // indices into Edges
	InEdges  map[LocalID][]int
}
