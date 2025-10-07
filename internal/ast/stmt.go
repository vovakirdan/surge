package ast

import (
	"surge/internal/source"
)

type StmtKind uint8

const (
	StmtBlock StmtKind = iota
	StmtLet
	StmtReturn
	StmtContinue
	StmtBreak
	StmtIf
	StmtWhile
	StmtFor
	StmtIn
	StmtFinally
)

type Stmt struct {
	Kind StmtKind
	Span source.Span
}

type Stmts struct {
	Arena *Arena[Stmt]
}

func NewStmts(capHint uint) *Stmts {
	return &Stmts{
		Arena: NewArena[Stmt](capHint),
	}
}

func (s *Stmts) New(kind StmtKind, span source.Span) StmtID {
	return StmtID(s.Arena.Allocate(Stmt{
		Kind: kind,
		Span: span,
	}))
}

func (s *Stmts) Get(id StmtID) *Stmt {
	return s.Arena.Get(uint32(id))
}
