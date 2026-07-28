package hir

import (
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// StmtKind enumerates HIR statement kinds.
type StmtKind uint8

const (
	// StmtLet represents variable declaration (let x = ...).
	StmtLet StmtKind = iota
	// StmtExpr represents an expression statement.
	StmtExpr
	// StmtAssign represents assignment (lhs = rhs).
	StmtAssign
	// StmtReturn represents return statement (always explicit in HIR).
	StmtReturn
	// StmtRet represents a block-local return statement.
	StmtRet
	// StmtBreak represents break statement.
	StmtBreak
	// StmtContinue represents continue statement.
	StmtContinue
	// StmtIf represents if/else statement.
	StmtIf
	// StmtWhile represents while loop.
	StmtWhile
	// StmtFor represents for loop (both classic and for-in).
	// Preserved as-is, desugaring happens in later stages.
	StmtFor
	// StmtBlock represents a nested block.
	StmtBlock
	// StmtDrop represents explicit drop (@drop expr).
	StmtDrop
	// StmtEnvelopeRelease frees a heap box synthesized AFTER sema computed
	// drop obligations, so the box never acquires a normal scope-exit
	// drop: a for-loop's per-step Option<T> envelope, its iterator cursor
	// (array/range state), or a `compare` expression's boxed-union
	// scrutinee temp. Two shapes selected by EnvelopeReleaseData.Cursor:
	// a SHALLOW free of the box using its own declared type's layout
	// (payload already moved out elsewhere, never recursed into — for-loop
	// step envelope, compare scrutinee whose arm bound the payload out),
	// or the iterator's FIXED protocol layout (cursor, independent of the
	// declared type). A `compare` scrutinee whose arm did NOT move the
	// payload out uses StmtDrop instead (deep drop through the union's
	// drop glue), not this channel.
	StmtEnvelopeRelease
)

// String returns a human-readable name for the statement kind.
func (k StmtKind) String() string {
	switch k {
	case StmtLet:
		return "Let"
	case StmtExpr:
		return "Expr"
	case StmtAssign:
		return "Assign"
	case StmtReturn:
		return "Return"
	case StmtRet:
		return "Ret"
	case StmtBreak:
		return "Break"
	case StmtContinue:
		return "Continue"
	case StmtIf:
		return "If"
	case StmtWhile:
		return "While"
	case StmtFor:
		return "For"
	case StmtBlock:
		return "Block"
	case StmtDrop:
		return "Drop"
	case StmtEnvelopeRelease:
		return "EnvelopeRelease"
	default:
		return "Unknown"
	}
}

// Stmt represents an HIR statement.
type Stmt struct {
	Kind StmtKind
	Span source.Span
	Data StmtData // Kind-specific payload
}

// StmtData is the interface for statement-specific data.
type StmtData interface {
	stmtData()
}

// LetData holds data for StmtLet.
type LetData struct {
	Name      string           // Variable name (empty for pattern destructuring)
	SymbolID  symbols.SymbolID // Symbol for this binding
	Type      types.TypeID     // Declared or inferred type
	Value     *Expr            // Initializer (nil if none)
	IsMut     bool             // true for 'let mut'
	IsConst   bool             // true for 'const' (treated as immutable let)
	Ownership Ownership        // Ownership of the binding
	Pattern   *Expr            // For tuple destructuring (nil for simple let)
}

func (LetData) stmtData() {}

// ExprStmtData holds data for StmtExpr.
type ExprStmtData struct {
	Expr *Expr
}

func (ExprStmtData) stmtData() {}

// AssignData holds data for StmtAssign.
type AssignData struct {
	Target *Expr // LHS
	Value  *Expr // RHS
}

func (AssignData) stmtData() {}

// DropLocal names a binding whose owned value drops at a synthesized
// point (scope-exit synthesis). MIR resolves the symbol to its local.
type DropLocal struct {
	SymbolID symbols.SymbolID
	Type     types.TypeID
	Span     source.Span
}

// ReturnData holds data for StmtReturn.
type ReturnData struct {
	Value      *Expr // nil for bare return
	IsTail     bool  // true if this return is the tail (normal) exit for a body
	IsImplicit bool  // true if this is a synthesized block return
	// DropsAfterValue lists live droppable bindings this return exits:
	// they free AFTER the return value evaluates (it may borrow them)
	// and before the terminator.
	DropsAfterValue []DropLocal
}

func (ReturnData) stmtData() {}

// RetData holds data for StmtRet.
type RetData struct {
	Value *Expr // nil for bare ret
	// DropsAfterValue lists live droppable bindings this ret exits, with
	// the same contract StmtReturn carries: they free AFTER the value
	// evaluates (it may read them) and before the jump to the block's
	// exit. A crossing body reaches its exit through `ret` and nothing
	// else, so without this list nothing it still owns is ever released.
	DropsAfterValue []DropLocal
}

func (RetData) stmtData() {}

// BreakData holds data for StmtBreak.
type BreakData struct {
	// Label could be added here for labeled breaks
}

func (BreakData) stmtData() {}

// ContinueData holds data for StmtContinue.
type ContinueData struct {
	// Label could be added here for labeled continues
}

func (ContinueData) stmtData() {}

// IfStmtData holds data for StmtIf.
type IfStmtData struct {
	Cond *Expr
	Then *Block
	Else *Block // nil if no else branch
}

func (IfStmtData) stmtData() {}

// WhileData holds data for StmtWhile.
type WhileData struct {
	Cond *Expr
	Body *Block
}

func (WhileData) stmtData() {}

// ForKind distinguishes between for loop variants.
type ForKind uint8

const (
	// ForClassic represents a C-style for loop.
	ForClassic ForKind = iota // for init; cond; post { ... }
	// ForIn represents a for-in loop.
	ForIn // for pattern in iterable { ... }
)

// ForData holds data for StmtFor.
type ForData struct {
	Kind ForKind

	// For classic for (ForClassic):
	Init *Stmt // nil if none
	Cond *Expr // nil if none
	Post *Expr // nil if none

	// For for-in (ForIn):
	VarName  string           // Loop variable name
	VarSym   symbols.SymbolID // Loop variable symbol
	VarType  types.TypeID     // Loop variable type
	Iterable *Expr            // Expression to iterate over

	// Common:
	Body *Block
}

func (ForData) stmtData() {}

// BlockStmtData holds data for StmtBlock.
type BlockStmtData struct {
	Block *Block
}

func (BlockStmtData) stmtData() {}

// DropData holds data for StmtDrop.
type DropData struct {
	Value *Expr
}

func (DropData) stmtData() {}

// EnvelopeReleaseData holds data for StmtEnvelopeRelease. Value must be a
// variable reference to the local being released. Cursor selects which
// fixed-shape free the backend performs:
//   - false: a shallow box-only free using the value's OWN declared
//     type's layout, never recursing into the payload — the
//     iterator-protocol step envelope (an Option<T> box, payload already
//     moved to the loop binding) or a `compare` scrutinee whose taken
//     arm bound the payload out (payload now owned by that binding).
//   - true: the iterator cursor (array or range state) — freed using
//     the iterator protocol's fixed struct layout, independent of
//     Value's declared type (which only exists to type-check and does
//     not describe the runtime cursor's real shape).
type EnvelopeReleaseData struct {
	Value  *Expr
	Cursor bool
}

func (EnvelopeReleaseData) stmtData() {}
