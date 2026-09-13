package mir

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/mono"
	"surge/internal/symbols"
)

// prepareBorrowedParams leaves ABI classification with the ordinary call path.
// Bodies without counted borrowed parameters need no concrete-HIR preparation.
func (l *funcLowerer) prepareBorrowedParams(fn *hir.Func) (*hir.Func, []symbols.SymbolID, error) {
	if fn.IsAsync() || fn.IsIntrinsic() || fn.Body == nil {
		return fn, nil, nil
	}
	var candidates []symbols.SymbolID
	for _, p := range fn.Params {
		if p.SymbolID.IsValid() && l.byValueArgContract(p.Type, false) == ArgContractBorrow && l.isRefCounted(p.Type) {
			candidates = append(candidates, p.SymbolID)
		}
	}
	if len(candidates) == 0 {
		return fn, nil, nil
	}
	return mono.PrepareBorrowedParamBody(fn, l.types, candidates)
}

func (l *funcLowerer) bindFunctionParams(fn *hir.Func) {
	l.f.ParamCount = len(fn.Params)
	for _, p := range fn.Params {
		if p.SymbolID.IsValid() {
			l.ensureLocal(p.SymbolID, p.Name, p.Type, p.Span)
			continue
		}
		name := p.Name
		if name == "" {
			name = "_"
		}
		addLocal(l.f, name, p.Type, l.localFlags(p.Type))
	}
	if l.f.IsAsync && l.types != nil {
		scopeType := l.types.Builtins().Uint64
		l.scopeLocal = addLocal(l.f, "__scope", scopeType, localFlagsFor(l.types, l.sema, scopeType))
		l.f.ScopeLocal = l.scopeLocal
	}
}

// bindBorrowedParamOwners copies into fresh ordinary locals after all ABI slots
// exist. The caller still owns each incoming slot; body writes and cleanup use
// the separate owner through the existing symbol map and SEMA drop plans.
func (l *funcLowerer) bindBorrowedParamOwners(working []symbols.SymbolID) error {
	for _, sym := range working {
		incoming, ok := l.symToLocal[sym]
		if !ok || incoming < 0 || int(incoming) >= l.f.ParamCount || int(incoming) >= len(l.f.Locals) {
			return fmt.Errorf("mir: borrowed parameter %d has no ABI local in %s", sym, l.f.Name)
		}
		param := l.f.Locals[incoming]
		if param.Sym != sym || l.byValueArgContract(param.Type, false) != ArgContractBorrow || !l.isRefCounted(param.Type) {
			return fmt.Errorf("mir: parameter %d is not a counted borrowed ABI local in %s", sym, l.f.Name)
		}
		// addLocal has no SymbolID and is not a temp-drop owner. Only the symbol
		// redirect below associates this owner with the body's cleanup plans.
		work := addLocal(l.f, "__param_work_"+param.Name, param.Type, l.localFlags(param.Type))
		l.f.Locals[work].Span = param.Span
		l.emit(&Instr{
			Kind: InstrAssign,
			Assign: AssignInstr{
				Dst: Place{Local: work},
				Src: RValue{Kind: RValueUse, Use: Operand{Kind: OperandRetain, Type: param.Type, Place: Place{Local: incoming}}},
			},
		})
		l.symToLocal[sym] = work
	}
	return nil
}
