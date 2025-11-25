package symbols

import (
	"surge/internal/ast"
	"surge/internal/source"
)

// ScopeKind enumerates supported scope categories.
type ScopeKind uint8

const (
	ScopeInvalid  ScopeKind = iota
	ScopeFile               // artificial root per parsed file
	ScopeModule             // module-level (top-level declarations)
	ScopeFunction           // function body scope
	ScopeBlock              // generic block scope
)

func (k ScopeKind) String() string {
	switch k {
	case ScopeFile:
		return "file"
	case ScopeModule:
		return "module"
	case ScopeFunction:
		return "function"
	case ScopeBlock:
		return "block"
	default:
		return "invalid"
	}
}

// ScopeOwnerKind distinguishes what AST element owns a scope.
type ScopeOwnerKind uint8

const (
	ScopeOwnerUnknown ScopeOwnerKind = iota
	ScopeOwnerFile
	ScopeOwnerItem
	ScopeOwnerStmt
	ScopeOwnerExpr
)

// ScopeOwner references an AST construct associated with the scope.
type ScopeOwner struct {
	Kind       ScopeOwnerKind
	SourceFile source.FileID
	ASTFile    ast.FileID
	Item       ast.ItemID
	Extern     ast.ExternMemberID
	Stmt       ast.StmtID
	Expr       ast.ExprID
}

// Scope models a lexical scope with a parent-child hierarchy.
type Scope struct {
	Kind      ScopeKind
	Parent    ScopeID
	Owner     ScopeOwner
	Span      source.Span
	NameIndex map[source.StringID][]SymbolID
	Symbols   []SymbolID
	Children  []ScopeID
}
