package symbols

import (
	"surge/internal/ast"
	"surge/internal/source"
)

// SymbolKind classifies the semantic meaning of a symbol.
type SymbolKind uint8

const (
	SymbolInvalid SymbolKind = iota
	SymbolModule
	SymbolImport
	SymbolFunction
	SymbolLet
	SymbolType
	SymbolParam
	SymbolTag
)

// SymbolFlags encode misc attributes for quick checks.
type SymbolFlags uint16

const (
	SymbolFlagPublic SymbolFlags = 1 << iota
	SymbolFlagMutable
	SymbolFlagImported
)

// SymbolDecl focuses on the AST origin for diagnostics.
type SymbolDecl struct {
	SourceFile source.FileID
	ASTFile    ast.FileID
	Item       ast.ItemID
	Stmt       ast.StmtID
	Expr       ast.ExprID
}

// Symbol describes a named entity available in a scope.
type Symbol struct {
	Name     source.StringID
	Kind     SymbolKind
	Scope    ScopeID
	Span     source.Span
	Flags    SymbolFlags
	Decl     SymbolDecl
	Aliases  []source.StringID
	Requires []SymbolID // optional dependencies (e.g., import group)
}
