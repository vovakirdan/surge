package mir

import (
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
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
	// A synthetic async block has parameters but no symbol-table entries for
	// them, so the name scan above finds nothing. ParamCount is the authority;
	// leaning on __scope's index instead would make the packed set depend on
	// where that local happened to land.
	if f.ParamCount > paramCount {
		paramCount = f.ParamCount
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

// operandForAsyncStateStore hands a live local into the resume payload that
// becomes its only owner while the function is suspended. A local can be Copy
// AND own heap — a reference-counted scalar, or a @copy value composite with
// one as a member, which is what LocalFlagOwnsHeap beside LocalFlagCopy says —
// and this position transfers what the local owns rather than duplicating it,
// so it must use MOVE just like a move-only local. Source-level copies have
// already been materialized as distinct owned locals before this synthetic
// handoff.
func operandForAsyncStateStore(f *Func, id LocalID) Operand {
	op := operandForLocal(f, id)
	if f == nil || id == NoLocalID || int(id) < 0 || int(id) >= len(f.Locals) {
		return op
	}
	if f.Locals[id].Flags&LocalFlagOwnsHeap != 0 {
		op.Kind = OperandMove
	}
	return op
}

// operandForAsyncInitialStateStore is the constructor-side half of the frame
// handoff. Unlike a resumed local, a reference-counted scalar parameter is
// borrowed at function entry: its caller keeps the original reference. The
// initial task frame therefore needs a RETAIN of its own. All other heap-owning
// parameters are owned at entry and transfer their existing value with MOVE.
func operandForAsyncInitialStateStore(f *Func, id LocalID, typesIn *types.Interner) Operand {
	op := operandForAsyncStateStore(f, id)
	if f == nil || typesIn == nil || id == NoLocalID || int(id) < 0 || int(id) >= len(f.Locals) {
		return op
	}
	if typesIn.IsRefCountedScalar(resolveAlias(typesIn, f.Locals[id].Type)) {
		op.Kind = OperandRetain
	}
	return op
}
