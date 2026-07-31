package mir

import (
	"surge/internal/source"
	"surge/internal/symbols"
)

func paramLocalSet(f *Func, symTable *symbols.Table) localSet {
	set := localSet{}
	if f == nil {
		return set
	}
	paramNames := map[string]struct{}{}
	paramCount := -1
	if symTable != nil && symTable.Symbols != nil {
		if sym := symTable.Symbols.Get(f.Sym); sym != nil && sym.Signature != nil {
			paramCount = len(sym.Signature.Params)
			if symTable.Strings != nil {
				for _, nameID := range sym.Signature.ParamNames {
					if nameID == source.NoStringID {
						continue
					}
					if name := symTable.Strings.MustLookup(nameID); name != "" {
						paramNames[name] = struct{}{}
					}
				}
			}
		}
	}
	if f.ScopeLocal != NoLocalID {
		scopeCount := int(f.ScopeLocal)
		if scopeCount > paramCount {
			paramCount = scopeCount
		}
	}
	for id, loc := range f.Locals {
		include := false
		if loc.Sym.IsValid() {
			if symTable != nil && symTable.Symbols != nil {
				sym := symTable.Symbols.Get(loc.Sym)
				if sym != nil && sym.Kind == symbols.SymbolParam {
					include = true
				}
			} else {
				include = true
			}
		}
		if !include {
			if paramCount >= 0 && id < paramCount {
				include = true
			} else if len(paramNames) > 0 {
				if _, ok := paramNames[loc.Name]; ok {
					include = true
				}
			}
		}
		if include {
			set.add(LocalID(id)) //nolint:gosec // bounded by locals length
		}
	}
	return set
}

func operandForLocal(f *Func, id LocalID) Operand {
	if f == nil || id == NoLocalID {
		return Operand{}
	}
	kind := OperandCopy
	if int(id) >= 0 && int(id) < len(f.Locals) {
		if f.Locals[id].Flags&LocalFlagCopy == 0 {
			kind = OperandMove
		}
	}
	return Operand{Kind: kind, Place: Place{Local: id}}
}
