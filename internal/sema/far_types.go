package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) resolveFarTypeExpr(id ast.TypeID, unary *ast.TypeUnary, scope symbols.ScopeID) types.TypeID {
	if unary == nil || tc.builder == nil || tc.types == nil {
		return types.NoTypeID
	}
	span := tc.typeSpan(id)
	if tc.rejectInvalidFarTypeExpr(unary.Inner, span) {
		return types.NoTypeID
	}
	inner := tc.resolveTypeExprWithScope(unary.Inner, scope)
	if inner == types.NoTypeID {
		return types.NoTypeID
	}
	if tc.isFarType(inner) {
		tc.report(diag.SemaFarNested, span, "nested `far` handles are not allowed")
		return types.NoTypeID
	}
	if tc.isArrayType(inner) {
		tc.report(diag.FutFarArrayPostponed, span, "array types cannot be used as `far` remote handles yet")
		return types.NoTypeID
	}
	if !tc.isRemoteHandleCapableType(inner) {
		tc.report(diag.SemaFarNonCapability, span, "`far` requires a remote-handle-capable type, got %s", tc.typeLabel(inner))
		return types.NoTypeID
	}
	return tc.types.Intern(types.MakeFar(inner))
}

func (tc *typeChecker) rejectInvalidFarTypeExpr(inner ast.TypeID, span source.Span) bool {
	if !inner.IsValid() || tc.builder == nil {
		return false
	}
	expr := tc.builder.Types.Get(inner)
	if expr == nil {
		return false
	}
	switch expr.Kind {
	case ast.TypeExprUnary:
		unary, ok := tc.builder.Types.UnaryType(inner)
		if !ok || unary == nil {
			return false
		}
		switch unary.Op {
		case ast.TypeUnaryFar:
			tc.report(diag.SemaFarNested, span, "nested `far` handles are not allowed")
			return true
		case ast.TypeUnaryOwn:
			tc.report(diag.SemaFarRemoteOwn, span, "`far own T` is invalid; move `own T` through `on` or `spawn on`")
			return true
		case ast.TypeUnaryRef, ast.TypeUnaryRefMut:
			tc.report(diag.SemaFarRemoteBorrow, span, "`far &T` and `far &mut T` are invalid remote lifetimes")
			return true
		case ast.TypeUnaryPointer:
			tc.report(diag.SemaRawPointerNotAllowed, span, "`far *T` is not allowed")
			return true
		}
	case ast.TypeExprFn:
		tc.report(diag.FutFarFnHandle, span, "function types cannot be used as `far` remote handles yet")
		return true
	case ast.TypeExprArray:
		tc.report(diag.FutFarArrayPostponed, span, "array types cannot be used as `far` remote handles yet")
		return true
	case ast.TypeExprPath:
		if tc.isExternTypePath(inner) {
			tc.report(diag.SemaFarExternTarget, span, "`extern<T>` is not a value capability for `far`")
			return true
		}
	}
	return false
}

func (tc *typeChecker) isExternTypePath(id ast.TypeID) bool {
	path, ok := tc.builder.Types.Path(id)
	if !ok || path == nil || len(path.Segments) != 1 {
		return false
	}
	return tc.lookupName(path.Segments[0].Name) == "extern"
}

func (tc *typeChecker) isFarType(id types.TypeID) bool {
	if id == types.NoTypeID || tc.types == nil {
		return false
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	return ok && tt.Kind == types.KindFar
}

func (tc *typeChecker) isRemoteHandleCapableType(id types.TypeID) bool {
	if id == types.NoTypeID {
		return false
	}
	if tc.isTaskType(id) || tc.isChannelType(id) {
		return true
	}
	if tc.typeHasAttr(tc.resolveAlias(id), "shard_pinned") {
		return true
	}
	return tc.typeNameIs(id, "TcpConn")
}

func (tc *typeChecker) typeNameIs(id types.TypeID, want string) bool {
	if id == types.NoTypeID || want == "" || tc.types == nil {
		return false
	}
	resolved := tc.resolveAlias(id)
	if info, ok := tc.types.StructInfo(resolved); ok && info != nil {
		return tc.lookupTypeName(resolved, info.Name) == want
	}
	if info, ok := tc.types.AliasInfo(resolved); ok && info != nil {
		return tc.lookupTypeName(resolved, info.Name) == want
	}
	return false
}

func (tc *typeChecker) reportFarLocalOp(id types.TypeID, span source.Span) bool {
	if !tc.isFarType(id) {
		return false
	}
	tc.report(diag.SemaFarLocalOp, span, "operation on %s requires an accepted remote context", tc.typeLabel(id))
	return true
}
