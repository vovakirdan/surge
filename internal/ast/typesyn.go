package ast

import (
	"surge/internal/source"
)

type TypeExprKind uint8

const (
	TypeExprPath TypeExprKind = iota
	TypeExprFn
	TypeExprGeneric
	TypeExprArray
	TypeExprTuple
)

type TypeExpr struct {
	Kind TypeExprKind
	Span source.Span
}

type TypeExprs struct {
	Arena *Arena[TypeExpr]
}

func NewTypeExprs(capHint uint) *TypeExprs {
	return &TypeExprs{
		Arena: NewArena[TypeExpr](capHint),
	}
}

func (t *TypeExprs) New(kind TypeExprKind, span source.Span) TypeID {
	return TypeID(t.Arena.Allocate(TypeExpr{
		Kind: kind,
		Span: span,
	}))
}

func (t *TypeExprs) Get(id TypeID) *TypeExpr {
	return t.Arena.Get(uint32(id))
}
