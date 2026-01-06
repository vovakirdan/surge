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

// ReturnData holds data for StmtReturn.
type ReturnData struct {
	Value  *Expr // nil for bare return
	IsTail bool  // true if this return is the tail (normal) exit for a body
}

func (ReturnData) stmtData() {}

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
