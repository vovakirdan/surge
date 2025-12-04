package symbols

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

// SymbolKind classifies the semantic meaning of a symbol.
type SymbolKind uint8

const (
	SymbolInvalid SymbolKind = iota
	SymbolModule
	SymbolImport
	SymbolFunction
	SymbolLet
	SymbolConst
	SymbolType
	SymbolParam
	SymbolTag
	SymbolContract
)

// SymbolFlags encode misc attributes for quick checks.
type SymbolFlags uint16

const (
	SymbolFlagPublic SymbolFlags = 1 << iota
	SymbolFlagMutable
	SymbolFlagImported
	SymbolFlagBuiltin
	SymbolFlagMethod
	SymbolFlagFilePrivate
	SymbolFlagEntrypoint
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
	case SymbolConst:
		return "const"
	case SymbolType:
		return "type"
	case SymbolParam:
		return "param"
	case SymbolTag:
		return "tag"
	case SymbolContract:
		return "contract"
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
	if f&SymbolFlagMethod != 0 {
		labels = append(labels, "method")
	}
	if f&SymbolFlagFilePrivate != 0 {
		labels = append(labels, "file-private")
	}
	if f&SymbolFlagEntrypoint != 0 {
		labels = append(labels, "entrypoint")
	}
	return labels
}

// BoundInstance stores a resolved contract reference and its type arguments.
type BoundInstance struct {
	Contract    SymbolID
	GenericArgs []types.TypeID
	Span        source.Span
}

// TypeParamSymbol describes a generic parameter and its bounds.
type TypeParamSymbol struct {
	Name      source.StringID
	Span      source.Span
	Bounds    []BoundInstance
	IsConst   bool
	ConstType types.TypeID
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
	Name             source.StringID
	Kind             SymbolKind
	Scope            ScopeID
	Span             source.Span
	Flags            SymbolFlags
	Decl             SymbolDecl
	Type             types.TypeID
	Aliases          []source.StringID
	Requires         []SymbolID // optional dependencies (e.g., import group)
	Signature        *FunctionSignature
	ModulePath       string
	ImportName       source.StringID
	Receiver         ast.TypeID
	ReceiverKey      TypeKey
	TypeParams       []source.StringID
	TypeParamSpan    source.Span
	TypeParamSymbols []TypeParamSymbol
	Contract         *ContractSpec
}
