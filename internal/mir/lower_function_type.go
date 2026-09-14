package mir

import (
	"fmt"

	"surge/internal/symbols"
	"surge/internal/types"
)

// A missing expression type must not erase the original callable's declared
// return-source relation when its parameter/result types become concrete.
func (l *funcLowerer) lowerFunctionValueType(symbol symbols.SymbolID, known types.TypeID) (types.TypeID, error) {
	if known != types.NoTypeID {
		return known, nil
	}
	if l == nil || l.mono == nil || l.mono.FuncBySym == nil || l.types == nil {
		return types.NoTypeID, nil
	}
	mf := l.mono.FuncBySym[symbol]
	if mf == nil {
		return types.NoTypeID, nil
	}
	if mf.Func == nil {
		// Imported/intrinsic instances without HIR keep their existing path.
		if l.symbols != nil && l.symbols.Table != nil && l.symbols.Table.Symbols != nil {
			if original := l.symbols.Table.Symbols.Get(mf.OrigSym); original != nil && original.Type != types.NoTypeID {
				return original.Type, nil
			}
		}
		return types.NoTypeID, nil
	}
	if l.symbols == nil || l.symbols.Table == nil || l.symbols.Table.Symbols == nil {
		return types.NoTypeID, fmt.Errorf("mir: function value: missing original function descriptor")
	}
	original := l.symbols.Table.Symbols.Get(mf.OrigSym)
	if original == nil || original.Type == types.NoTypeID {
		return types.NoTypeID, fmt.Errorf("mir: function value %d: missing original function descriptor for symbol %d", mf.InstanceSym, mf.OrigSym)
	}
	originalType := resolveAlias(l.types, original.Type)
	info, ok := l.types.FnInfo(originalType)
	if !ok || info == nil {
		return types.NoTypeID, fmt.Errorf("mir: function value %d: original symbol %d has no function descriptor", mf.InstanceSym, mf.OrigSym)
	}
	if len(info.Params) != len(mf.Func.Params) {
		return types.NoTypeID, fmt.Errorf("mir: function value %d: arity mismatch between original symbol %d (%d) and concrete function (%d)",
			mf.InstanceSym, mf.OrigSym, len(info.Params), len(mf.Func.Params))
	}
	params := make([]types.TypeID, len(mf.Func.Params))
	for i, param := range mf.Func.Params {
		params[i] = param.Type
	}
	return l.types.RebuildFn(originalType, params, mf.Func.Result), nil
}
