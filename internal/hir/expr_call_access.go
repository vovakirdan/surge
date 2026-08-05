package hir

import (
	"surge/internal/sema"
	"surge/internal/symbols"
)

// CallData holds data for ExprCall.
type CallData struct {
	Callee           *Expr   // The function/method being called
	Args             []*Expr // Arguments
	SymbolID         symbols.SymbolID
	DeferredUseID    sema.DeferredUseID
	CrossingDispatch bool
	// SelectDispatch marks the outer send/recv/await/timeout-shaped call in
	// a select/race arm header. It is a structural descriptor consumed by
	// MIR, not an executable callable. Its receiver and arguments remain
	// ordinary expressions and are still traversed by mono.
	SelectDispatch bool
}

func (CallData) exprData() {}

// FieldAccessData holds data for ExprFieldAccess.
type FieldAccessData struct {
	Object    *Expr
	FieldName string
	FieldIdx  int // Struct field index, -1 if unknown
	// MoveOut marks a read that TAKES the field out of its container: the
	// place leaves, the container drops only what remains, and this read is
	// the value's transfer rather than a second holder of it. An ordinary
	// read is the opposite on every count, and the two lower to the same
	// shape, so the mode has to travel with the read.
	MoveOut bool
}

func (FieldAccessData) exprData() {}

// IndexData holds data for ExprIndex.
type IndexData struct {
	Object *Expr
	Index  *Expr
}

func (IndexData) exprData() {}
