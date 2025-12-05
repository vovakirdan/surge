package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) validateFunctionCall(sym *symbols.Symbol, call *ast.ExprCallData, argTypes []types.TypeID) {
	if sym == nil || call == nil || tc.builder == nil {
		return
	}
	fnItem, ok := tc.builder.Items.Fn(sym.Decl.Item)
	if !ok || fnItem == nil {
		return
	}
	bindings := tc.inferTypeParamBindings(sym, fnItem, argTypes, call)
	if len(sym.TypeParamSymbols) > 0 {
		tc.enforceContractBounds(sym.TypeParamSymbols, bindings, tc.exprSpan(call.Target))
	}
}

func (tc *typeChecker) inferTypeParamBindings(sym *symbols.Symbol, fn *ast.FnItem, argTypes []types.TypeID, call *ast.ExprCallData) map[source.StringID]bindingInfo {
	if sym == nil || fn == nil || len(sym.TypeParams) == 0 || tc.builder == nil || call == nil {
		return nil
	}
	result := make(map[source.StringID]bindingInfo, len(sym.TypeParams))
	indexByName := make(map[source.StringID]struct{}, len(sym.TypeParams))
	for _, name := range sym.TypeParams {
		indexByName[name] = struct{}{}
	}
	paramIDs := tc.builder.Items.GetFnParamIDs(fn)
	for i, pid := range paramIDs {
		if i >= len(argTypes) || i >= len(call.Args) {
			break
		}
		argType := argTypes[i]
		if argType == types.NoTypeID {
			continue
		}
		argSpan := tc.exprSpan(call.Args[i])
		argSym := tc.symbolForExpr(call.Args[i])
		argValType := tc.valueType(argType)
		if argSym.IsValid() {
			if boundType := tc.bindingType(argSym); boundType != types.NoTypeID {
				argValType = boundType
			}
		}
		param := tc.builder.Items.FnParam(pid)
		if param == nil {
			continue
		}
		if name := tc.paramTypeParamName(param.Type, indexByName); name != source.NoStringID {
			result[name] = bindingInfo{typ: argValType, span: argSpan, sym: argSym}
		}
	}
	return result
}

func (tc *typeChecker) paramTypeParamName(typeID ast.TypeID, allowed map[source.StringID]struct{}) source.StringID {
	if typeID == ast.NoTypeID || tc.builder == nil {
		return source.NoStringID
	}
	expr := tc.builder.Types.Get(typeID)
	if expr == nil || expr.Kind != ast.TypeExprPath {
		return source.NoStringID
	}
	path, ok := tc.builder.Types.Path(typeID)
	if !ok || path == nil || len(path.Segments) != 1 {
		return source.NoStringID
	}
	seg := path.Segments[0]
	if len(seg.Generics) > 0 {
		return source.NoStringID
	}
	if _, ok := allowed[seg.Name]; ok {
		return seg.Name
	}
	return source.NoStringID
}

func (tc *typeChecker) enforceContractBounds(params []symbols.TypeParamSymbol, bindings map[source.StringID]bindingInfo, span source.Span) {
	if len(params) == 0 || tc.reporter == nil {
		return
	}
	for _, param := range params {
		binding := bindings[param.Name]
		concrete := binding.typ
		if concrete == types.NoTypeID {
			continue
		}
		reportSpan := binding.span
		if reportSpan == (source.Span{}) {
			reportSpan = span
		}
		typeLabel := tc.bindingTypeLabel(binding)
		for _, bound := range param.Bounds {
			inst := bound
			inst.GenericArgs = tc.substituteBoundArgs(bound.GenericArgs, bindings)
			if tc.typeParamSatisfiesBound(concrete, inst, bindings) {
				continue
			}
			tc.checkContractSatisfaction(concrete, inst, reportSpan, typeLabel)
		}
	}
}

func (tc *typeChecker) substituteBoundArgs(args []types.TypeID, bindings map[source.StringID]bindingInfo) []types.TypeID {
	if len(args) == 0 {
		return nil
	}
	out := make([]types.TypeID, len(args))
	for i, arg := range args {
		out[i] = tc.substituteTypeParamByName(arg, bindings)
	}
	return out
}

func (tc *typeChecker) substituteTypeParamByName(id types.TypeID, bindings map[source.StringID]bindingInfo) types.TypeID {
	if id == types.NoTypeID || tc.types == nil {
		return id
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return resolved
	}
	if tt.Kind == types.KindGenericParam {
		name := tc.typeParamNames[resolved]
		if name == source.NoStringID {
			if info, okInfo := tc.types.TypeParamInfo(resolved); okInfo && info != nil {
				name = info.Name
				if name != source.NoStringID {
					tc.typeParamNames[resolved] = name
				}
			}
		}
		if name != source.NoStringID {
			if concrete := bindings[name].typ; concrete != types.NoTypeID {
				return concrete
			}
		}
		return resolved
	}
	if tt.Kind == types.KindStruct {
		if elem, ok := tc.arrayElemType(resolved); ok {
			inner := tc.substituteTypeParamByName(elem, bindings)
			if inner == elem {
				return resolved
			}
			return tc.instantiateArrayType(inner)
		}
	}
	switch tt.Kind {
	case types.KindPointer, types.KindReference, types.KindOwn:
		elem := tc.substituteTypeParamByName(tt.Elem, bindings)
		if elem == tt.Elem {
			return resolved
		}
		clone := tt
		clone.Elem = elem
		return tc.types.Intern(clone)
	case types.KindArray:
		elem := tc.substituteTypeParamByName(tt.Elem, bindings)
		if elem == tt.Elem {
			return resolved
		}
		clone := tt
		clone.Elem = elem
		return tc.types.Intern(clone)
	default:
		return resolved
	}
}
