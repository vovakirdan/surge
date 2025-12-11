package symbols

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

// ExportedSymbol captures metadata about a symbol exported from a module.
type ExportedSymbol struct {
	Name           string
	NameID         source.StringID
	Kind           SymbolKind
	Flags          SymbolFlags
	Span           source.Span
	Signature      *FunctionSignature
	ReceiverKey    TypeKey
	TypeParams     []source.StringID
	TypeParamNames []string
	TypeParamSpan  source.Span
	TypeParamSyms  []TypeParamSymbol
	Type           types.TypeID
	Contract       *ContractSpec
}

// ModuleExports aggregates exported symbols for a module, preserving overload sets.
type ModuleExports struct {
	Path        string
	Symbols     map[string][]ExportedSymbol
	PragmaFlags ast.PragmaFlags // Pragma flags from the module (e.g., PragmaFlagDirective)
}

// NewModuleExports creates an exports container for the given module path.
func NewModuleExports(path string) *ModuleExports {
	return &ModuleExports{
		Path:    path,
		Symbols: make(map[string][]ExportedSymbol),
	}
}

// Add registers an exported symbol under its textual name.
func (m *ModuleExports) Add(sym *ExportedSymbol) {
	if m == nil {
		return
	}
	if sym == nil {
		return
	}
	m.Symbols[sym.Name] = append(m.Symbols[sym.Name], *sym)
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

	// Capture pragma flags from the file for directive validation
	if file := builder.Files.Get(res.File); file != nil {
		exports.PragmaFlags = file.Pragma.Flags
	}

	for _, symID := range scope.Symbols {
		sym := res.Table.Symbols.Get(symID)
		if sym == nil {
			continue
		}
		if res.ModuleFiles != nil {
			if _, ok := res.ModuleFiles[sym.Decl.ASTFile]; !ok {
				continue
			}
		} else if sym.Decl.ASTFile != res.File {
			continue
		}
		if sym.Flags&SymbolFlagFilePrivate != 0 {
			continue
		}
		if sym.Flags&SymbolFlagPublic == 0 && sym.Flags&SymbolFlagBuiltin == 0 {
			continue
		}
		if !isExportableKind(sym.Kind) {
			continue
		}
		name := builder.StringsInterner.MustLookup(sym.Name)
		exports.Add(&ExportedSymbol{
			Name:           name,
			NameID:         sym.Name,
			Kind:           sym.Kind,
			Flags:          sym.Flags,
			Span:           sym.Span,
			Type:           sym.Type,
			Signature:      sym.Signature,
			ReceiverKey:    sym.ReceiverKey,
			TypeParams:     append([]source.StringID(nil), sym.TypeParams...),
			TypeParamNames: lookupNames(builder, sym.TypeParams),
			TypeParamSpan:  sym.TypeParamSpan,
			TypeParamSyms:  CloneTypeParamSymbols(sym.TypeParamSymbols),
			Contract:       cloneContractSpec(sym.Contract),
		})
	}
	return exports
}

func lookupNames(builder *ast.Builder, ids []source.StringID) []string {
	if builder == nil || builder.StringsInterner == nil || len(ids) == 0 {
		return nil
	}
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == source.NoStringID {
			continue
		}
		result = append(result, builder.StringsInterner.MustLookup(id))
	}
	return result
}

func isExportableKind(kind SymbolKind) bool {
	switch kind {
	case SymbolFunction, SymbolTag, SymbolType, SymbolLet, SymbolConst, SymbolContract:
		return true
	default:
		return false
	}
}

func CloneTypeParamSymbols(params []TypeParamSymbol) []TypeParamSymbol {
	if len(params) == 0 {
		return nil
	}
	out := make([]TypeParamSymbol, 0, len(params))
	for _, p := range params {
		copyBounds := make([]BoundInstance, 0, len(p.Bounds))
		for _, b := range p.Bounds {
			copyBounds = append(copyBounds, BoundInstance{
				Contract:    b.Contract,
				GenericArgs: append([]types.TypeID(nil), b.GenericArgs...),
				Span:        b.Span,
			})
		}
		out = append(out, TypeParamSymbol{
			Name:      p.Name,
			Span:      p.Span,
			Bounds:    copyBounds,
			IsConst:   p.IsConst,
			ConstType: p.ConstType,
		})
	}
	return out
}
