package symbols

import (
	"surge/internal/ast"
	"surge/internal/source"
)

// ScopeKind enumerates supported scope categories.
type ScopeKind uint8

const (
	// ScopeInvalid represents an uninitialized or erroneous scope.
	ScopeInvalid ScopeKind = iota
	// ScopeFile represents an artificial root scope per parsed file.
	ScopeFile // artificial root per parsed file
	// ScopeModule represents module-level (top-level declarations).
	ScopeModule // module-level (top-level declarations)
	// ScopeFunction represents function body scope.
	ScopeFunction // function body scope
	// ScopeBlock represents a block scope.
	ScopeBlock // generic block scope
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
	// ScopeOwnerUnknown indicates the owner was not identified.
	ScopeOwnerUnknown ScopeOwnerKind = iota
	// ScopeOwnerFile indicates the scope is owned by a file.
	ScopeOwnerFile
	// ScopeOwnerItem indicates the scope is owned by an item.
	ScopeOwnerItem
	// ScopeOwnerStmt indicates the scope is owned by a statement.
	ScopeOwnerStmt
	// ScopeOwnerExpr indicates the scope is owned by an expression.
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
