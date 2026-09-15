package mir

import (
	"surge/internal/symbols"
	"surge/internal/types"
)

func (l *funcLowerer) emitNoResultCall(callee *Callee, args []Operand, contracts []ArgContract, resultType types.TypeID) Operand {
	l.emit(&Instr{Kind: InstrCall, Call: CallInstr{
		HasDst:       false,
		Callee:       *callee,
		Args:         args,
		ArgContracts: contracts,
	}})
	if l.calleeIsCoreIntrinsicExit(callee, len(args)) {
		l.setTerm(&Terminator{Kind: TermUnreachable})
	}
	return l.constNothing(resultType)
}

// Only the canonical core intrinsic exits the process. An ordinary nothing
// result or a matching display name does not establish that contract; follow
// the concrete instance back to its original builtin symbol and HIR declaration.
func (l *funcLowerer) calleeIsCoreIntrinsicExit(callee *Callee, argCount int) bool {
	if l == nil || l.types == nil || callee.Kind != CalleeSym || !callee.Sym.IsValid() || argCount != 1 || l.mono == nil || l.mono.Source == nil {
		return false
	}
	mf := l.mono.FuncBySym[callee.Sym]
	if mf == nil || mf.Func == nil || mf.InstanceSym != callee.Sym || mf.Func.SymbolID != callee.Sym || !mf.OrigSym.IsValid() {
		return false
	}
	if !mf.Func.IsIntrinsic() || len(mf.Func.Params) != 1 || mf.Func.Result == types.NoTypeID || !l.isNothingType(mf.Func.Result) {
		return false
	}
	source := l.mono.Source
	if source.Symbols == nil || source.Symbols.Table == nil || source.Symbols.Table.Symbols == nil || source.Symbols.Table.Strings == nil {
		return false
	}
	table := source.Symbols.Table
	original := table.Symbols.Get(mf.OrigSym)
	if original == nil || original.Kind != symbols.SymbolFunction || original.Flags&symbols.SymbolFlagBuiltin == 0 || original.ModulePath != "core" {
		return false
	}
	name, ok := table.Strings.Lookup(original.Name)
	if !ok || name != "exit" {
		return false
	}
	for _, fn := range l.mono.Source.Funcs {
		if fn != nil && fn.SymbolID == mf.OrigSym {
			return fn.IsIntrinsic() && len(fn.Params) == 1 && fn.Result != types.NoTypeID && l.isNothingType(fn.Result)
		}
	}
	return false
}
