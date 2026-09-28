package mir

import (
	"surge/internal/hir"
	"surge/internal/mono"
	"surge/internal/symbols"
)

// bodilessDeclarations collects the functions and extern methods declared
// without a body whose symbol does not carry the builtin flag, which every
// `@intrinsic` declaration and every core symbol carries. Sema refuses such a
// free function unless an `@override` implementation completes it (a call then
// reaches the implementation's own symbol); an extern<T> member stays legal
// without a provider. The set lets a backend refuse a call of one by symbol
// rather than guess a builtin from its name. A call names the mono instance,
// so every instance of such a declaration is in the set as well.
func bodilessDeclarations(mm *mono.MonoModule) map[symbols.SymbolID]string {
	out := bodilessSourceDeclarations(mm.Source)
	for _, key := range mm.SortedFuncKeys() {
		mf := mm.Funcs[key]
		if mf == nil || mf.Func != nil && (mf.Func.Body != nil || mf.Func.IsIntrinsic()) {
			continue
		}
		name, declared := out[mf.OrigSym]
		if !declared {
			continue
		}
		if mf.InstanceSym.IsValid() {
			out[mf.InstanceSym] = name
		}
		if mf.Func != nil && mf.Func.SymbolID.IsValid() {
			out[mf.Func.SymbolID] = name
		}
	}
	return out
}

func bodilessSourceDeclarations(src *hir.Module) map[symbols.SymbolID]string {
	if src == nil || src.Symbols == nil || src.Symbols.Table == nil || src.Symbols.Table.Symbols == nil {
		return map[symbols.SymbolID]string{}
	}
	table := src.Symbols.Table
	out := make(map[symbols.SymbolID]string)
	for i := 1; i <= table.Symbols.Len(); i++ {
		id := symbols.SymbolID(i)
		sym := table.Symbols.Get(id)
		if sym == nil || sym.Kind != symbols.SymbolFunction || sym.Signature == nil || sym.Signature.HasBody ||
			sym.Flags&symbols.SymbolFlagBuiltin != 0 {
			continue
		}
		name := "_"
		if table.Strings != nil {
			if spelled, ok := table.Strings.Lookup(sym.Name); ok {
				name = spelled
			}
		}
		out[id] = name
	}
	return out
}
