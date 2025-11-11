package symbols

import (
	"surge/internal/ast"
	"surge/internal/source"
)

// ExportedSymbol captures metadata about a symbol exported from a module.
type ExportedSymbol struct {
	Name  string
	Kind  SymbolKind
	Flags SymbolFlags
	Span  source.Span
}

// ModuleExports aggregates exported symbols for a module, preserving overload sets.
type ModuleExports struct {
	Path    string
	Symbols map[string][]ExportedSymbol
}

// NewModuleExports creates an exports container for the given module path.
func NewModuleExports(path string) *ModuleExports {
	return &ModuleExports{
		Path:    path,
		Symbols: make(map[string][]ExportedSymbol),
	}
}

// Add registers an exported symbol under its textual name.
func (m *ModuleExports) Add(sym ExportedSymbol) {
	if m == nil {
		return
	}
	m.Symbols[sym.Name] = append(m.Symbols[sym.Name], sym)
}

// Lookup returns the overload set for the given name, if any.
func (m *ModuleExports) Lookup(name string) []ExportedSymbol {
	if m == nil {
		return nil
	}
	return m.Symbols[name]
}

// CollectExports builds module exports from the resolver result and AST builder.
func CollectExports(builder *ast.Builder, res Result, modulePath string) *ModuleExports {
	if builder == nil || res.Table == nil || !res.FileScope.IsValid() {
		return nil
	}
	scope := res.Table.Scopes.Get(res.FileScope)
	if scope == nil {
		return nil
	}
	exports := NewModuleExports(modulePath)
	for _, symID := range scope.Symbols {
		sym := res.Table.Symbols.Get(symID)
		if sym == nil || !isExportableKind(sym.Kind) {
			continue
		}
		name := builder.StringsInterner.MustLookup(sym.Name)
		exports.Add(ExportedSymbol{
			Name:  name,
			Kind:  sym.Kind,
			Flags: sym.Flags,
			Span:  sym.Span,
		})
	}
	return exports
}

func isExportableKind(kind SymbolKind) bool {
	switch kind {
	case SymbolFunction, SymbolTag, SymbolType, SymbolLet:
		return true
	default:
		return false
	}
}
