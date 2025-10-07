package ast

import (
	"surge/internal/source"
)

type ExprKind uint8

const (
	ExprIdent ExprKind = iota
	ExprLit
	ExprCall
	ExprBinary
	ExprUnary
	ExprGroup
	ExprIndex
	ExprTernary
	ExprAwait
	ExprSignal
	ExprParallel
	ExprCompare
)

type Expr struct {
	Kind ExprKind
	Span source.Span
}

type Exprs struct {
	Arena *Arena[Expr]
}

func NewExprs(capHint uint) *Exprs {
	return &Exprs{
		Arena: NewArena[Expr](capHint),
	}
}

func (e *Exprs) New(kind ExprKind, span source.Span) ExprID {
	return ExprID(e.Arena.Allocate(Expr{
		Kind: kind,
		Span: span,
	}))
}

func (e *Exprs) Get(id ExprID) *Expr {
	return e.Arena.Get(uint32(id))
}
