package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) resolveTypeExprWithScope(id ast.TypeID, scope symbols.ScopeID) types.TypeID {
	if !id.IsValid() || tc.builder == nil {
		return types.NoTypeID
	}
	scope = tc.scopeOrFile(scope)
	key := typeCacheKey{Type: id, Scope: scope, Env: tc.currentTypeParamEnv()}
	if tc.typeCache != nil {
		if cached, ok := tc.typeCache[key]; ok {
			return cached
		}
	}
	expr := tc.builder.Types.Get(id)
	if expr == nil {
		return types.NoTypeID
	}
	var result types.TypeID
	switch expr.Kind {
	case ast.TypeExprPath:
		path, _ := tc.builder.Types.Path(id)
		result = tc.resolveTypePath(path, expr.Span, scope)
	case ast.TypeExprUnary:
		if unary, ok := tc.builder.Types.UnaryType(id); ok && unary != nil {
			inner := tc.resolveTypeExprWithScope(unary.Inner, scope)
			if inner != types.NoTypeID {
				switch unary.Op {
				case ast.TypeUnaryOwn:
					result = tc.types.Intern(types.MakeOwn(inner))
				case ast.TypeUnaryRef:
					result = tc.types.Intern(types.MakeReference(inner, false))
				case ast.TypeUnaryRefMut:
					result = tc.types.Intern(types.MakeReference(inner, true))
				case ast.TypeUnaryPointer:
					result = tc.types.Intern(types.MakePointer(inner))
				}
			}
		}
	case ast.TypeExprArray:
		if arr, ok := tc.builder.Types.Array(id); ok && arr != nil {
			elem := tc.resolveTypeExprWithScope(arr.Elem, scope)
			if elem != types.NoTypeID {
				count := types.ArrayDynamicLength
				if arr.Kind == ast.ArraySized {
					if !arr.HasConstLen {
						tc.report(diag.SemaTypeMismatch, expr.Span, "array length must be a constant")
						break
					}
					if arr.ConstLength > uint64(^uint32(0)) {
						tc.report(diag.SemaTypeMismatch, expr.Span, "array length %d exceeds limit", arr.ConstLength)
						break
					}
					count = uint32(arr.ConstLength)
				}
				result = tc.types.Intern(types.MakeArray(elem, count))
			}
		}
	case ast.TypeExprOptional:
		if opt, ok := tc.builder.Types.Optional(id); ok && opt != nil {
			inner := tc.resolveTypeExprWithScope(opt.Inner, scope)
			result = tc.resolveOptionType(inner, expr.Span, scope)
		}
	case ast.TypeExprErrorable:
		if errable, ok := tc.builder.Types.Errorable(id); ok && errable != nil {
			inner := tc.resolveTypeExprWithScope(errable.Inner, scope)
			var errType types.TypeID
			if errable.Error.IsValid() {
				errType = tc.resolveTypeExprWithScope(errable.Error, scope)
			} else {
				errType = tc.resolveErrorType(expr.Span, scope)
			}
			result = tc.resolveResultType(inner, errType, expr.Span, scope)
		}
	default:
		// other type forms (tuple/fn) are not supported yet
	}
	if tc.typeCache != nil {
		tc.typeCache[key] = result
	}
	return result
}

func (tc *typeChecker) resolveTypePath(path *ast.TypePath, span source.Span, scope symbols.ScopeID) types.TypeID {
	if path == nil || len(path.Segments) == 0 {
		return types.NoTypeID
	}
	if len(path.Segments) > 1 {
		tc.report(diag.SemaUnresolvedSymbol, span, "qualified type paths are not supported yet")
		return types.NoTypeID
	}
	seg := path.Segments[0]
	if len(seg.Generics) == 0 {
		if param := tc.lookupTypeParam(seg.Name); param != types.NoTypeID {
			return param
		}
	}
	args := tc.resolveTypeArgs(seg.Generics, scope)
	return tc.resolveNamedType(seg.Name, args, span, scope)
}

func (tc *typeChecker) resolveNamedType(name source.StringID, args []types.TypeID, span source.Span, scope symbols.ScopeID) types.TypeID {
	if name == source.NoStringID {
		return types.NoTypeID
	}
	literal := tc.lookupName(name)
	if literal != "" {
		if builtin := tc.builtinTypeByName(literal); builtin != types.NoTypeID {
			return builtin
		}
	}
	symID := tc.lookupTypeSymbol(name, scope)
	if !symID.IsValid() {
		if literal == "" {
			literal = "_"
		}
		tc.report(diag.SemaUnresolvedSymbol, span, "unknown type %s", literal)
		return types.NoTypeID
	}
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return types.NoTypeID
	}
	expected := len(sym.TypeParams)
	if expected == 0 {
		if len(args) > 0 {
			tc.report(diag.SemaTypeMismatch, span, "%s does not take type arguments", tc.lookupName(sym.Name))
			return types.NoTypeID
		}
		return tc.symbolType(symID)
	}
	if len(args) == 0 {
		tc.report(diag.SemaTypeMismatch, span, "%s requires %d type argument(s)", tc.lookupName(sym.Name), expected)
		return types.NoTypeID
	}
	if len(args) != expected {
		tc.report(diag.SemaTypeMismatch, span, "%s expects %d type argument(s), got %d", tc.lookupName(sym.Name), expected, len(args))
		return types.NoTypeID
	}
	return tc.instantiateType(symID, args)
}

func (tc *typeChecker) resolveTypeArgs(typeIDs []ast.TypeID, scope symbols.ScopeID) []types.TypeID {
	if len(typeIDs) == 0 {
		return nil
	}
	args := make([]types.TypeID, 0, len(typeIDs))
	for _, tid := range typeIDs {
		arg := tc.resolveTypeExprWithScope(tid, scope)
		args = append(args, arg)
	}
	return args
}
