package hir

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Module represents an HIR module (corresponding to a source file).
type Module struct {
	Name      string      // Module name
	Path      string      // Module path
	SourceAST ast.FileID  // Link back to source AST
	Funcs     []*Func     // Functions in this module
	Types     []TypeDecl  // Type declarations
	Consts    []ConstDecl // Top-level constants
	Globals   []VarDecl   // Top-level let bindings

	// TypeInterner and BindingTypes are borrowed from sema results to support
	// HIR-side analysis and normalization passes without re-running sema.
	TypeInterner *types.Interner
	BindingTypes map[symbols.SymbolID]types.TypeID

	// Symbols holds the resolver table used when lowering/normalizing.
	// It is optional and used on a best-effort basis for debug-only passes.
	Symbols *symbols.Result
}

// TypeDecl represents a type declaration in HIR.
// We don't duplicate the full type structure - just reference the symbol/type.
type TypeDecl struct {
	Name     string           // Type name
	SymbolID symbols.SymbolID // Symbol table entry
	TypeID   types.TypeID     // Resolved type
	Span     source.Span      // Source location
	Kind     TypeDeclKind     // Kind of type declaration
}

// TypeDeclKind enumerates type declaration kinds.
type TypeDeclKind uint8

const (
	// TypeDeclStruct represents a struct declaration in HIR.
	TypeDeclStruct TypeDeclKind = iota
	// TypeDeclUnion represents a union declaration in HIR.
	TypeDeclUnion
	TypeDeclEnum
	TypeDeclAlias
	TypeDeclTag
	TypeDeclContract
)

// String returns a human-readable name for the type declaration kind.
func (k TypeDeclKind) String() string {
	switch k {
	case TypeDeclStruct:
		return "struct"
	case TypeDeclUnion:
		return "union"
	case TypeDeclEnum:
		return "enum"
	case TypeDeclAlias:
		return "alias"
	case TypeDeclTag:
		return "tag"
	case TypeDeclContract:
		return "contract"
	default:
		return "unknown"
	}
}

// ConstDecl represents a top-level constant declaration.
type ConstDecl struct {
	Name     string           // Constant name
	SymbolID symbols.SymbolID // Symbol table entry
	Type     types.TypeID     // Constant type
	Value    *Expr            // Constant value expression
	Span     source.Span      // Source location
}

// VarDecl represents a top-level variable declaration (let).
type VarDecl struct {
	Name     string           // Variable name
	SymbolID symbols.SymbolID // Symbol table entry
	Type     types.TypeID     // Variable type
	Value    *Expr            // Initializer (nil if none)
	IsMut    bool             // true for 'let mut'
	Span     source.Span      // Source location
}

// FindFunc finds a function by name, returns nil if not found.
func (m *Module) FindFunc(name string) *Func {
	for _, f := range m.Funcs {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// FindFuncBySymbol finds a function by symbol ID, returns nil if not found.
func (m *Module) FindFuncBySymbol(symID symbols.SymbolID) *Func {
	for _, f := range m.Funcs {
		if f.SymbolID == symID {
			return f
		}
	}
	return nil
}
