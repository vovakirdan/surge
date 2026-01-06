package ast

import "surge/internal/source"

// StmtKind enumerates the different kinds of statements.
type StmtKind uint8

const (
	// StmtBlock represents a block statement.
	StmtBlock StmtKind = iota
	// StmtLet represents a let statement.
	StmtLet
	// StmtConst represents a const statement.
	StmtConst
	// StmtExpr represents an expression statement.
	StmtExpr
	// StmtSignal represents a signal statement.
	StmtSignal
	// StmtReturn represents a return statement.
	StmtReturn
	// StmtBreak represents a break statement.
	StmtBreak
	StmtContinue
	StmtIf
	StmtWhile
	StmtForClassic
	StmtForIn
	StmtDrop
)

// Stmt represents a statement in the AST.
type Stmt struct {
	Kind StmtKind
	Span source.Span
	// Payload хранит индекс в соответствующей арене данных для конкретного statement.
	// Для statement'ов без дополнительных данных устанавливается в ast.NoPayloadID.
	Payload PayloadID
}

// Stmts manages allocation of statements.
type Stmts struct {
	Arena       *Arena[Stmt]
	Blocks      *Arena[BlockStmt]
	Lets        *Arena[LetStmt]
	Consts      *Arena[ConstStmt]
	Exprs       *Arena[ExprStmt]
	Signals     *Arena[SignalStmt]
	Returns     *Arena[ReturnStmt]
	Ifs         *Arena[IfStmt]
	Whiles      *Arena[WhileStmt]
	ClassicFors *Arena[ForClassicStmt]
	ForIns      *Arena[ForInStmt]
	Drops       *Arena[DropStmt]
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
		Arena:       NewArena[Stmt](capHint),
		Blocks:      NewArena[BlockStmt](capHint),
		Lets:        NewArena[LetStmt](capHint),
		Consts:      NewArena[ConstStmt](capHint),
		Exprs:       NewArena[ExprStmt](capHint),
		Signals:     NewArena[SignalStmt](capHint),
		Returns:     NewArena[ReturnStmt](capHint),
		Ifs:         NewArena[IfStmt](capHint),
		Whiles:      NewArena[WhileStmt](capHint),
		ClassicFors: NewArena[ForClassicStmt](capHint),
		ForIns:      NewArena[ForInStmt](capHint),
		Drops:       NewArena[DropStmt](capHint),
	}
}

// New creates a new statement with the given kind and payload.
func (s *Stmts) New(kind StmtKind, span source.Span, payload PayloadID) StmtID {
	return StmtID(s.Arena.Allocate(Stmt{
		Kind:    kind,
		Span:    span,
		Payload: payload,
	}))
}

// Get returns the statement with the given ID.
func (s *Stmts) Get(id StmtID) *Stmt {
	return s.Arena.Get(uint32(id))
}

// BlockStmt represents a block of statements { ... }.
type BlockStmt struct {
	Stmts []StmtID
}

// LetStmt represents a variable declaration using 'let' or 'mut'.
type LetStmt struct {
	Name    source.StringID // Used for simple `let x = ...`
	Pattern ExprID          // Used for `let (x, y) = ...` (ExprTuple of ExprIdent)
	Type    TypeID
	Value   ExprID
	IsMut   bool
}

// ConstStmt represents a constant declaration statement.
type ConstStmt struct {
	Name  source.StringID
	Type  TypeID
	Value ExprID
}

// ExprStmt represents an expression used as a statement.
type ExprStmt struct {
	Expr             ExprID
	MissingSemicolon bool
}

// DropStmt represents a 'drop' statement.
type DropStmt struct {
	Expr ExprID
}

// SignalStmt represents a signal emission statement (deprecated or internal).
type SignalStmt struct {
	Name  source.StringID
	Value ExprID
}

// ReturnStmt represents a 'return' statement.
type ReturnStmt struct {
	Expr ExprID
}

// IfStmt represents an 'if' statement.
type IfStmt struct {
	Cond ExprID
	Then StmtID
	Else StmtID
}

// WhileStmt represents a 'while' loop statement.
type WhileStmt struct {
	Cond ExprID
	Body StmtID
}

// ForClassicStmt represents a C-style 'for' loop.
type ForClassicStmt struct {
	Init StmtID
	Cond ExprID
	Post ExprID
	Body StmtID
}

// ForInStmt represents a 'for ... in' loop.
type ForInStmt struct {
	Pattern     source.StringID
	PatternSpan source.Span
	Type        TypeID
	Iterable    ExprID
	Body        StmtID
}

// NewBlock creates a new block statement.
func (s *Stmts) NewBlock(span source.Span, stmts []StmtID) StmtID {
	payload := PayloadID(s.Blocks.Allocate(BlockStmt{
		Stmts: append([]StmtID(nil), stmts...),
	}))
	return s.New(StmtBlock, span, payload)
}

// Block returns the block statement data for the given StmtID.
func (s *Stmts) Block(id StmtID) *BlockStmt {
	stmt := s.Get(id)
	if stmt == nil || stmt.Kind != StmtBlock || !stmt.Payload.IsValid() {
		return nil
	}
	return s.Blocks.Get(uint32(stmt.Payload))
}

// NewLet creates a new let statement.
func (s *Stmts) NewLet(span source.Span, name source.StringID, pattern ExprID, typ TypeID, value ExprID, isMut bool) StmtID {
	payload := PayloadID(s.Lets.Allocate(LetStmt{
		Name:    name,
		Pattern: pattern,
		Type:    typ,
		Value:   value,
		IsMut:   isMut,
	}))
	return s.New(StmtLet, span, payload)
}

// Let returns the let statement data for the given StmtID.
func (s *Stmts) Let(id StmtID) *LetStmt {
	stmt := s.Get(id)
	if stmt == nil || stmt.Kind != StmtLet || !stmt.Payload.IsValid() {
		return nil
	}
	return s.Lets.Get(uint32(stmt.Payload))
}

// NewConst creates a new const statement.
func (s *Stmts) NewConst(span source.Span, name source.StringID, typ TypeID, value ExprID) StmtID {
	payload := PayloadID(s.Consts.Allocate(ConstStmt{
		Name:  name,
		Type:  typ,
		Value: value,
	}))
	return s.New(StmtConst, span, payload)
}

// Const returns the const statement data for the given StmtID.
func (s *Stmts) Const(id StmtID) *ConstStmt {
	stmt := s.Get(id)
	if stmt == nil || stmt.Kind != StmtConst || !stmt.Payload.IsValid() {
		return nil
	}
	return s.Consts.Get(uint32(stmt.Payload))
}

// NewExpr creates a new expression statement.
func (s *Stmts) NewExpr(span source.Span, expr ExprID, missingSemicolon bool) StmtID {
	payload := PayloadID(s.Exprs.Allocate(ExprStmt{
		Expr:             expr,
		MissingSemicolon: missingSemicolon,
	}))
	return s.New(StmtExpr, span, payload)
}

// Expr returns the expression statement data for the given StmtID.
func (s *Stmts) Expr(id StmtID) *ExprStmt {
	stmt := s.Get(id)
	if stmt == nil || stmt.Kind != StmtExpr || !stmt.Payload.IsValid() {
		return nil
	}
	return s.Exprs.Get(uint32(stmt.Payload))
}

// NewDrop creates a new drop statement.
func (s *Stmts) NewDrop(span source.Span, expr ExprID) StmtID {
	payload := PayloadID(s.Drops.Allocate(DropStmt{
		Expr: expr,
	}))
	return s.New(StmtDrop, span, payload)
}

// Drop returns the drop statement data for the given StmtID.
func (s *Stmts) Drop(id StmtID) *DropStmt {
	stmt := s.Get(id)
	if stmt == nil || stmt.Kind != StmtDrop || !stmt.Payload.IsValid() {
		return nil
	}
	return s.Drops.Get(uint32(stmt.Payload))
}

// NewSignal creates a new signal statement.
func (s *Stmts) NewSignal(span source.Span, name source.StringID, value ExprID) StmtID {
	payload := PayloadID(s.Signals.Allocate(SignalStmt{
		Name:  name,
		Value: value,
	}))
	return s.New(StmtSignal, span, payload)
}

// Signal returns the signal statement data for the given StmtID.
func (s *Stmts) Signal(id StmtID) *SignalStmt {
	stmt := s.Get(id)
	if stmt == nil || stmt.Kind != StmtSignal || !stmt.Payload.IsValid() {
		return nil
	}
	return s.Signals.Get(uint32(stmt.Payload))
}

// NewReturn creates a new return statement.
func (s *Stmts) NewReturn(span source.Span, expr ExprID) StmtID {
	payload := PayloadID(s.Returns.Allocate(ReturnStmt{
		Expr: expr,
	}))
	return s.New(StmtReturn, span, payload)
}

// Return returns the return statement data for the given StmtID.
func (s *Stmts) Return(id StmtID) *ReturnStmt {
	stmt := s.Get(id)
	if stmt == nil || stmt.Kind != StmtReturn || !stmt.Payload.IsValid() {
		return nil
	}
	return s.Returns.Get(uint32(stmt.Payload))
}

// NewBreak creates a new break statement.
func (s *Stmts) NewBreak(span source.Span) StmtID {
	return s.New(StmtBreak, span, NoPayloadID)
}

// NewContinue creates a new continue statement.
func (s *Stmts) NewContinue(span source.Span) StmtID {
	return s.New(StmtContinue, span, NoPayloadID)
}

// NewIf creates a new if statement.
func (s *Stmts) NewIf(span source.Span, cond ExprID, thenStmt, elseStmt StmtID) StmtID {
	payload := PayloadID(s.Ifs.Allocate(IfStmt{
		Cond: cond,
		Then: thenStmt,
		Else: elseStmt,
	}))
	return s.New(StmtIf, span, payload)
}

// If returns the if statement data for the given StmtID.
func (s *Stmts) If(id StmtID) *IfStmt {
	stmt := s.Get(id)
	if stmt == nil || stmt.Kind != StmtIf || !stmt.Payload.IsValid() {
		return nil
	}
	return s.Ifs.Get(uint32(stmt.Payload))
}

// NewWhile creates a new while loop statement.
func (s *Stmts) NewWhile(span source.Span, cond ExprID, body StmtID) StmtID {
	payload := PayloadID(s.Whiles.Allocate(WhileStmt{
		Cond: cond,
		Body: body,
	}))
	return s.New(StmtWhile, span, payload)
}

// While returns the while loop statement data for the given StmtID.
func (s *Stmts) While(id StmtID) *WhileStmt {
	stmt := s.Get(id)
	if stmt == nil || stmt.Kind != StmtWhile || !stmt.Payload.IsValid() {
		return nil
	}
	return s.Whiles.Get(uint32(stmt.Payload))
}

// NewForClassic creates a new C-style for loop statement.
func (s *Stmts) NewForClassic(span source.Span, init StmtID, cond, post ExprID, body StmtID) StmtID {
	payload := PayloadID(s.ClassicFors.Allocate(ForClassicStmt{
		Init: init,
		Cond: cond,
		Post: post,
		Body: body,
	}))
	return s.New(StmtForClassic, span, payload)
}

// ForClassic returns the C-style for loop statement data for the given StmtID.
func (s *Stmts) ForClassic(id StmtID) *ForClassicStmt {
	stmt := s.Get(id)
	if stmt == nil || stmt.Kind != StmtForClassic || !stmt.Payload.IsValid() {
		return nil
	}
	return s.ClassicFors.Get(uint32(stmt.Payload))
}

// NewForIn creates a new for-in loop statement.
func (s *Stmts) NewForIn(span source.Span, pattern source.StringID, patternSpan source.Span, typ TypeID, iterable ExprID, body StmtID) StmtID {
	payload := PayloadID(s.ForIns.Allocate(ForInStmt{
		Pattern:     pattern,
		PatternSpan: patternSpan,
		Type:        typ,
		Iterable:    iterable,
		Body:        body,
	}))
	return s.New(StmtForIn, span, payload)
}

// ForIn returns the for-in loop statement data for the given StmtID.
func (s *Stmts) ForIn(id StmtID) *ForInStmt {
	stmt := s.Get(id)
	if stmt == nil || stmt.Kind != StmtForIn || !stmt.Payload.IsValid() {
		return nil
	}
	return s.ForIns.Get(uint32(stmt.Payload))
}
