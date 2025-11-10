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
	SymbolFlagBuiltin
)

func (k SymbolKind) String() string {
	switch k {
	case SymbolModule:
		return "module"
	case SymbolImport:
		return "import"
	case SymbolFunction:
		return "function"
	case SymbolLet:
		return "let"
	case SymbolType:
		return "type"
	case SymbolParam:
		return "param"
	case SymbolTag:
		return "tag"
	default:
		return "invalid"
	}
}

// Strings returns a slice of textual flag labels.
func (f SymbolFlags) Strings() []string {
	if f == 0 {
		return nil
	}
	labels := make([]string, 0, 4)
	if f&SymbolFlagPublic != 0 {
		labels = append(labels, "public")
	}
	if f&SymbolFlagMutable != 0 {
		labels = append(labels, "mutable")
	}
	if f&SymbolFlagImported != 0 {
		labels = append(labels, "imported")
	}
	if f&SymbolFlagBuiltin != 0 {
		labels = append(labels, "builtin")
	}
	return labels
}

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
	Name       source.StringID
	Kind       SymbolKind
	Scope      ScopeID
	Span       source.Span
	Flags      SymbolFlags
	Decl       SymbolDecl
	Aliases    []source.StringID
	Requires   []SymbolID // optional dependencies (e.g., import group)
	Signature  *FunctionSignature
	ModulePath string
	ImportName source.StringID
}
