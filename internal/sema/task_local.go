package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/symbols"
)

func (tc *typeChecker) markLocalTaskBinding(symID symbols.SymbolID) {
	if !symID.IsValid() {
		return
	}
	if tc.localTaskBindings == nil {
		tc.localTaskBindings = make(map[symbols.SymbolID]struct{})
	}
	tc.localTaskBindings[symID] = struct{}{}
}

func (tc *typeChecker) clearLocalTaskBinding(symID symbols.SymbolID) {
	if !symID.IsValid() || tc.localTaskBindings == nil {
		return
	}
	delete(tc.localTaskBindings, symID)
}

func (tc *typeChecker) updateLocalTaskBindingFromExpr(symID symbols.SymbolID, exprID ast.ExprID) {
	if !symID.IsValid() {
		return
	}
	if tc.isLocalTaskExpr(exprID) {
		tc.markLocalTaskBinding(symID)
		return
	}
	tc.clearLocalTaskBinding(symID)
}

func (tc *typeChecker) updateLocalTaskBindingFromAssign(left, right ast.ExprID) {
	left = tc.unwrapGroupExpr(left)
	expr := tc.builder.Exprs.Get(left)
	if expr == nil || expr.Kind != ast.ExprIdent {
		return
	}
	symID := tc.symbolForExpr(left)
	tc.updateLocalTaskBindingFromExpr(symID, right)
}

func (tc *typeChecker) isLocalTaskBinding(symID symbols.SymbolID) bool {
	if !symID.IsValid() {
		return false
	}
	if tc.taskTracker != nil && tc.taskTracker.IsLocalBinding(symID) {
		return true
	}
	_, ok := tc.localTaskBindings[symID]
	return ok
}

func (tc *typeChecker) isLocalTaskExpr(exprID ast.ExprID) bool {
	if !exprID.IsValid() {
		return false
	}
	for {
		exprID = tc.unwrapGroupExpr(exprID)
		expr := tc.builder.Exprs.Get(exprID)
		if expr == nil {
			return false
		}
		switch expr.Kind {
		case ast.ExprSpawn:
			return tc.spawnHasAttr(exprID, "local")
		case ast.ExprIdent:
			return tc.isLocalTaskBinding(tc.symbolForExpr(exprID))
		case ast.ExprUnary:
			if data, ok := tc.builder.Exprs.Unary(exprID); ok && data != nil {
				exprID = data.Operand
				continue
			}
			return false
		default:
			return false
		}
	}
}

func (tc *typeChecker) spawnHasAttr(exprID ast.ExprID, name string) bool {
	if tc.builder == nil || tc.builder.Items == nil || tc.builder.Exprs == nil {
		return false
	}
	spawn, ok := tc.builder.Exprs.Spawn(exprID)
	if !ok || spawn == nil || spawn.AttrCount == 0 || !spawn.AttrStart.IsValid() {
		return false
	}
	attrs := tc.builder.Items.CollectAttrs(spawn.AttrStart, spawn.AttrCount)
	for _, attr := range attrs {
		attrName := tc.lookupName(attr.Name)
		if strings.EqualFold(attrName, name) {
			return true
		}
	}
	return false
}
