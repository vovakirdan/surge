package ast

import "surge/internal/source"

type StmtKind uint8

const (
	StmtBlock StmtKind = iota
	StmtLet
	StmtExpr
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
	// Payload хранит индекс в соответствующей арене данных для конкретного statement.
	// Для statement'ов без дополнительных данных устанавливается в ast.NoPayloadID.
	Payload PayloadID
}

type Stmts struct {
	Arena   *Arena[Stmt]
	Blocks  *Arena[BlockStmt]
	Lets    *Arena[LetStmt]
	Exprs   *Arena[ExprStmt]
	Returns *Arena[ReturnStmt]
}

// NewStmts creates and returns a new Stmts populated with internal arenas.
// If capHint is 0, a default capacity of 1<<8 is used. The returned Stmts
// has separate arenas allocated for Stmt, BlockStmt, LetStmt, ExprStmt and
// ReturnStmt using the provided capacity hint.
func NewStmts(capHint uint) *Stmts {
	if capHint == 0 {
		capHint = 1 << 8
	}
	return &Stmts{
		Arena:   NewArena[Stmt](capHint),
		Blocks:  NewArena[BlockStmt](capHint),
		Lets:    NewArena[LetStmt](capHint),
		Exprs:   NewArena[ExprStmt](capHint),
		Returns: NewArena[ReturnStmt](capHint),
	}
}

func (s *Stmts) New(kind StmtKind, span source.Span, payload PayloadID) StmtID {
	return StmtID(s.Arena.Allocate(Stmt{
		Kind:    kind,
		Span:    span,
		Payload: payload,
	}))
}

func (s *Stmts) Get(id StmtID) *Stmt {
	return s.Arena.Get(uint32(id))
}

type BlockStmt struct {
	Stmts []StmtID
}

type LetStmt struct {
	Name  source.StringID
	Type  TypeID
	Value ExprID
	IsMut bool
}

type ExprStmt struct {
	Expr ExprID
}

type ReturnStmt struct {
	Expr ExprID
}

func (s *Stmts) NewBlock(span source.Span, stmts []StmtID) StmtID {
	payload := PayloadID(s.Blocks.Allocate(BlockStmt{
		Stmts: append([]StmtID(nil), stmts...),
	}))
	return s.New(StmtBlock, span, payload)
}

func (s *Stmts) Block(id StmtID) *BlockStmt {
	stmt := s.Get(id)
	if stmt == nil || stmt.Kind != StmtBlock || !stmt.Payload.IsValid() {
		return nil
	}
	return s.Blocks.Get(uint32(stmt.Payload))
}

func (s *Stmts) NewLet(span source.Span, name source.StringID, typ TypeID, value ExprID, isMut bool) StmtID {
	payload := PayloadID(s.Lets.Allocate(LetStmt{
		Name:  name,
		Type:  typ,
		Value: value,
		IsMut: isMut,
	}))
	return s.New(StmtLet, span, payload)
}

func (s *Stmts) Let(id StmtID) *LetStmt {
	stmt := s.Get(id)
	if stmt == nil || stmt.Kind != StmtLet || !stmt.Payload.IsValid() {
		return nil
	}
	return s.Lets.Get(uint32(stmt.Payload))
}

func (s *Stmts) NewExpr(span source.Span, expr ExprID) StmtID {
	payload := PayloadID(s.Exprs.Allocate(ExprStmt{
		Expr: expr,
	}))
	return s.New(StmtExpr, span, payload)
}

func (s *Stmts) Expr(id StmtID) *ExprStmt {
	stmt := s.Get(id)
	if stmt == nil || stmt.Kind != StmtExpr || !stmt.Payload.IsValid() {
		return nil
	}
	return s.Exprs.Get(uint32(stmt.Payload))
}

func (s *Stmts) NewReturn(span source.Span, expr ExprID) StmtID {
	payload := PayloadID(s.Returns.Allocate(ReturnStmt{
		Expr: expr,
	}))
	return s.New(StmtReturn, span, payload)
}

func (s *Stmts) Return(id StmtID) *ReturnStmt {
	stmt := s.Get(id)
	if stmt == nil || stmt.Kind != StmtReturn || !stmt.Payload.IsValid() {
		return nil
	}
	return s.Returns.Get(uint32(stmt.Payload))
}
