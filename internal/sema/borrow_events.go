package sema

import (
	"surge/internal/source"
	"surge/internal/symbols"
)

// BorrowEventKind identifies the type of borrow event recorded during analysis.
type BorrowEventKind uint8

const (
	// BorrowEvBorrowStart indicates the beginning of a borrow.
	BorrowEvBorrowStart BorrowEventKind = iota
	// BorrowEvBorrowEnd indicates the end of a borrow.
	BorrowEvBorrowEnd
	BorrowEvMove
	BorrowEvWrite
	BorrowEvDrop
	BorrowEvSpawnEscape
)

func (k BorrowEventKind) String() string {
	switch k {
	case BorrowEvBorrowStart:
		return "borrow_start"
	case BorrowEvBorrowEnd:
		return "borrow_end"
	case BorrowEvMove:
		return "move"
	case BorrowEvWrite:
		return "write"
	case BorrowEvDrop:
		return "drop"
	case BorrowEvSpawnEscape:
		return "spawn_escape"
	default:
		return "unknown"
	}
}

// BorrowEvent is a lightweight log entry produced while borrow checking.
// It is meant for downstream debug/visualization and must not affect diagnostics.
type BorrowEvent struct {
	Kind BorrowEventKind

	// Borrow is the borrow entry associated with this event (when applicable).
	Borrow BorrowID

	// BorrowKind is only meaningful for BorrowEvBorrowStart.
	BorrowKind BorrowKind

	// Place is the accessed place (when applicable).
	Place Place

	// Binding is the binding symbol involved in this event (when applicable),
	// e.g. @drop target or task-captured variable.
	Binding symbols.SymbolID

	Span  source.Span
	Scope symbols.ScopeID

	// Issue captures whether this event was blocked by an active borrow.
	Issue       BorrowIssueKind
	IssueBorrow BorrowID

	Note string
}
