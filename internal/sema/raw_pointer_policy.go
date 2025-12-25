package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

const rawPointerBackendOnlyMsg = "raw pointers are backend-only; use ownership/borrows or expose an intrinsic/extern API"

func (tc *typeChecker) resolveTypeExprWithScopeAllowPointer(id ast.TypeID, scope symbols.ScopeID, allow bool) types.TypeID {
	if !allow {
		return tc.resolveTypeExprWithScope(id, scope)
	}
	prev := tc.allowRawPointer
	tc.allowRawPointer = true
	resolved := tc.resolveTypeExprWithScope(id, scope)
	tc.allowRawPointer = prev
	return resolved
}

func (tc *typeChecker) hasIntrinsicAttr(start ast.AttrID, count uint32) bool {
	infos := tc.collectAttrs(start, count)
	_, ok := hasAttr(infos, "intrinsic")
	return ok
}

func (tc *typeChecker) checkRawPointerTypeExpr(id ast.TypeID) {
	if !id.IsValid() || tc.builder == nil {
		return
	}
	if tc.rawPointerChecked == nil {
		tc.rawPointerChecked = make(map[ast.TypeID]struct{})
	}
	tc.checkRawPointerTypeExprRec(id)
}

func (tc *typeChecker) checkRawPointerTypeExprRec(id ast.TypeID) {
	if !id.IsValid() || tc.builder == nil {
		return
	}
	if tc.rawPointerChecked != nil {
		if _, ok := tc.rawPointerChecked[id]; ok {
			return
		}
		tc.rawPointerChecked[id] = struct{}{}
	}
	expr := tc.builder.Types.Get(id)
	if expr == nil {
		return
	}
	switch expr.Kind {
	case ast.TypeExprPath:
		path, ok := tc.builder.Types.Path(id)
		if !ok || path == nil {
			return
		}
		for _, seg := range path.Segments {
			for _, arg := range seg.Generics {
				tc.checkRawPointerTypeExprRec(arg)
			}
		}
	case ast.TypeExprUnary:
		unary, ok := tc.builder.Types.UnaryType(id)
		if !ok || unary == nil {
			return
		}
		if unary.Op == ast.TypeUnaryPointer {
			tc.report(diag.SemaRawPointerNotAllowed, expr.Span, rawPointerBackendOnlyMsg)
		}
		tc.checkRawPointerTypeExprRec(unary.Inner)
	case ast.TypeExprArray:
		arr, ok := tc.builder.Types.Array(id)
		if ok && arr != nil {
			tc.checkRawPointerTypeExprRec(arr.Elem)
		}
	case ast.TypeExprTuple:
		tup, ok := tc.builder.Types.Tuple(id)
		if ok && tup != nil {
			for _, elem := range tup.Elems {
				tc.checkRawPointerTypeExprRec(elem)
			}
		}
	case ast.TypeExprFn:
		fn, ok := tc.builder.Types.Fn(id)
		if ok && fn != nil {
			for _, param := range fn.Params {
				tc.checkRawPointerTypeExprRec(param.Type)
			}
			tc.checkRawPointerTypeExprRec(fn.Return)
		}
	case ast.TypeExprOptional:
		opt, ok := tc.builder.Types.Optional(id)
		if ok && opt != nil {
			tc.checkRawPointerTypeExprRec(opt.Inner)
		}
	case ast.TypeExprErrorable:
		errable, ok := tc.builder.Types.Errorable(id)
		if ok && errable != nil {
			tc.checkRawPointerTypeExprRec(errable.Inner)
			if errable.Error.IsValid() {
				tc.checkRawPointerTypeExprRec(errable.Error)
			}
		}
	}
}
