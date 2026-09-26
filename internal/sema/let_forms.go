package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

// The two `let` forms that bind no simple name. Both live here rather than in
// the statement walk so that the walk stays the size it was.

// checkTupleLet refuses `let (x, y) = value`. A `let` binds one name
// (LANGUAGE.md §2.10 and the grammar's `Let`); tuple patterns are written only
// in `compare` arms. Owner ruling 2026-09-25.
//
// The value is still typed and moved, and every name of the pattern still gets
// its element's type when the shapes agree, so what follows reads a known
// binding and reports nothing about the refused pattern a second time.
func (tc *typeChecker) checkTupleLet(letStmt *ast.LetStmt, scope symbols.ScopeID) {
	valueType := tc.typeExpr(letStmt.Value)
	tc.observeMove(letStmt.Value, tc.exprSpan(letStmt.Value))
	span := tc.exprSpan(letStmt.Pattern)
	b := diag.ReportError(tc.reporter, diag.SemaLetTuplePattern, span, "a `let` binds one name; it cannot take a tuple apart")
	b.WithNote(span, "tuple patterns are written only in `compare` arms")
	b.WithHelp(span, "bind the tuple and read its elements: `let t = value; let x = t.0;`").Emit()
	tc.bindRefusedTuplePattern(letStmt.Pattern, valueType, scope)
}

// bindRefusedTuplePattern gives each name of a refused pattern its element's
// type when the pattern and the tuple agree in shape, and reports nothing: the
// pattern already carries its one error. A name it cannot type keeps no type.
func (tc *typeChecker) bindRefusedTuplePattern(pattern ast.ExprID, valueType types.TypeID, scope symbols.ScopeID) {
	tuple, ok := tc.builder.Exprs.Tuple(pattern)
	if !ok || tuple == nil {
		return
	}
	info, ok := tc.types.TupleInfo(tc.valueType(valueType))
	if !ok || info == nil || len(info.Elems) != len(tuple.Elements) {
		return
	}
	for i, elem := range tuple.Elements {
		node := tc.builder.Exprs.Get(elem)
		if node == nil {
			continue
		}
		switch node.Kind {
		case ast.ExprIdent:
			ident, _ := tc.builder.Exprs.Ident(elem)
			if ident == nil {
				continue
			}
			tc.result.ExprTypes[elem] = info.Elems[i]
			symID := tc.symbolForExpr(elem)
			if !symID.IsValid() && scope.IsValid() {
				symID = tc.symbolInScope(scope, ident.Name, symbols.SymbolLet)
			}
			if symID.IsValid() {
				tc.setBindingType(symID, info.Elems[i])
			}
		case ast.ExprTuple:
			tc.bindRefusedTuplePattern(elem, info.Elems[i], scope)
		}
	}
}

// checkDiscardedLet types `let _ = value`.
//
// `_` names nobody, so nobody receives the value: it is the discarded result
// the statement `value;` is, released at the end of this statement by the
// same temporary machinery, and a PLACE on the right is read, not moved --
// `x` stays with its binding. Binding it instead consumed the temporary on
// behalf of a binding that never dropped, and every owning value discarded
// through `_` leaked.
func (tc *typeChecker) checkDiscardedLet(letStmt *ast.LetStmt, scope symbols.ScopeID) {
	declaredType := tc.resolveTypeExprWithScope(letStmt.Type, scope)
	tc.pushDiscardedExpr(letStmt.Value)
	valueType := tc.typeExprWithExpected(letStmt.Value, declaredType)
	tc.refuseDroppedTasks(letStmt.Value)
	tc.popDiscardedExpr()
	tc.ensureBindingTypeMatch(letStmt.Type, declaredType, valueType, letStmt.Value)
}
