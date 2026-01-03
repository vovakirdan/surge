package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type taskContainerInfo struct {
	Scope   symbols.ScopeID
	Pending bool
	Span    source.Span
}

func (tc *typeChecker) isTaskContainerType(id types.TypeID) bool {
	return tc.containsTaskType(id) && !tc.isTaskType(id)
}

func (tc *typeChecker) containsTaskType(id types.TypeID) bool {
	if tc.types == nil {
		return false
	}
	seen := make(map[types.TypeID]struct{})
	return tc.containsTaskTypeVisited(tc.valueType(id), seen)
}

func (tc *typeChecker) containsTaskTypeVisited(id types.TypeID, seen map[types.TypeID]struct{}) bool {
	if id == types.NoTypeID || tc.types == nil {
		return false
	}
	id = tc.valueType(id)
	if id == types.NoTypeID {
		return false
	}
	if tc.isTaskType(id) {
		return true
	}
	if _, ok := seen[id]; ok {
		return false
	}
	seen[id] = struct{}{}
	if elem, ok := tc.arrayElemType(id); ok {
		return tc.containsTaskTypeVisited(elem, seen)
	}

	tt, ok := tc.types.Lookup(id)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindStruct:
		info, ok := tc.types.StructInfo(id)
		if !ok || info == nil {
			return false
		}
		for _, field := range info.Fields {
			if tc.containsTaskTypeVisited(field.Type, seen) {
				return true
			}
		}
	case types.KindUnion:
		info, ok := tc.types.UnionInfo(id)
		if !ok || info == nil {
			return false
		}
		for _, member := range info.Members {
			if tc.containsTaskTypeVisited(member.Type, seen) {
				return true
			}
		}
	case types.KindTuple:
		info, ok := tc.types.TupleInfo(id)
		if !ok || info == nil {
			return false
		}
		for _, elem := range info.Elems {
			if tc.containsTaskTypeVisited(elem, seen) {
				return true
			}
		}
	case types.KindAlias:
		target, ok := tc.types.AliasTarget(id)
		if !ok {
			return false
		}
		return tc.containsTaskTypeVisited(target, seen)
	}
	return false
}

func (tc *typeChecker) containerExprForStore(target ast.ExprID) ast.ExprID {
	if !target.IsValid() || tc.builder == nil {
		return ast.NoExprID
	}
	exprID := tc.unwrapGroupExpr(target)
	for exprID.IsValid() {
		if idx, ok := tc.builder.Exprs.Index(exprID); ok && idx != nil {
			exprID = idx.Target
			continue
		}
		return exprID
	}
	return ast.NoExprID
}

func (tc *typeChecker) taskContainerPlace(expr ast.ExprID) (Place, bool) {
	if !expr.IsValid() {
		return Place{}, false
	}
	desc, ok := tc.resolvePlace(expr)
	if !ok {
		return Place{}, false
	}
	desc, _ = tc.expandPlaceDescriptor(desc)
	if !desc.Base.IsValid() {
		return Place{}, false
	}
	place := tc.canonicalPlace(desc)
	if !place.IsValid() {
		return Place{}, false
	}
	return place, true
}

func (tc *typeChecker) markTaskContainerPending(place Place, span source.Span) {
	if !place.IsValid() {
		return
	}
	if tc.taskContainers == nil {
		tc.taskContainers = make(map[Place]*taskContainerInfo)
	}
	info := tc.taskContainers[place]
	if info == nil {
		scope := tc.currentScope()
		if sym := tc.symbolFromID(place.Base); sym != nil && sym.Scope.IsValid() {
			scope = sym.Scope
		}
		info = &taskContainerInfo{Scope: scope}
		tc.taskContainers[place] = info
	}
	info.Pending = true
	if info.Span == (source.Span{}) {
		info.Span = span
	}
}

func (tc *typeChecker) markTaskContainerConsumed(place Place) {
	if !place.IsValid() || tc.taskContainers == nil {
		return
	}
	if info := tc.taskContainers[place]; info != nil {
		info.Pending = false
	}
}

func (tc *typeChecker) markTaskContainerFromBinding(symID symbols.SymbolID, value ast.ExprID, valueType types.TypeID, span source.Span) {
	if !symID.IsValid() || !tc.isTaskContainerType(valueType) {
		return
	}
	dest := Place{Base: symID}
	if value.IsValid() {
		if src, ok := tc.taskContainerPlace(value); ok {
			if info := tc.taskContainers[src]; info != nil {
				tc.taskContainers[dest] = &taskContainerInfo{
					Scope:   info.Scope,
					Pending: info.Pending,
					Span:    info.Span,
				}
				delete(tc.taskContainers, src)
				return
			}
		}
	}
	if value.IsValid() && tc.builder != nil {
		if expr := tc.builder.Exprs.Get(value); expr != nil {
			switch expr.Kind {
			case ast.ExprArray, ast.ExprStruct:
				tc.markTaskContainerPending(dest, span)
			}
		}
	}
}

func (tc *typeChecker) trackTaskContainerStore(target, value ast.ExprID, valueType types.TypeID) {
	if !target.IsValid() || !tc.isTaskType(valueType) {
		return
	}
	containerExpr := tc.containerExprForStore(target)
	if !containerExpr.IsValid() {
		return
	}
	containerType := tc.typeExprAssignLHS(containerExpr)
	if !tc.isTaskContainerType(containerType) {
		return
	}
	place, ok := tc.taskContainerPlace(containerExpr)
	if !ok {
		return
	}
	tc.markTaskContainerPending(place, tc.exprSpan(target))
	tc.trackTaskPassedAsArg(value)
}

func (tc *typeChecker) trackTaskContainerAssign(target, value ast.ExprID, valueType types.TypeID, span source.Span) {
	if !target.IsValid() || !tc.isTaskContainerType(valueType) {
		return
	}
	place, ok := tc.taskContainerPlace(target)
	if !ok {
		return
	}
	if value.IsValid() {
		if src, ok := tc.taskContainerPlace(value); ok {
			if info := tc.taskContainers[src]; info != nil {
				tc.taskContainers[place] = &taskContainerInfo{
					Scope:   info.Scope,
					Pending: info.Pending,
					Span:    info.Span,
				}
				delete(tc.taskContainers, src)
				return
			}
		}
		if tc.builder != nil {
			if expr := tc.builder.Exprs.Get(value); expr != nil {
				switch expr.Kind {
				case ast.ExprArray, ast.ExprStruct:
					tc.markTaskContainerPending(place, span)
				}
			}
		}
	}
}

func (tc *typeChecker) checkTaskContainerEscape(expr ast.ExprID, exprType types.TypeID, span source.Span) {
	if !expr.IsValid() || !tc.isTaskContainerType(exprType) {
		return
	}
	tc.report(diag.SemaTaskLifetimeError, span, "task container cannot escape its scope")
}

func (tc *typeChecker) checkTaskContainersAtScopeExit(scope symbols.ScopeID) {
	if tc.taskContainers == nil {
		return
	}
	for place, info := range tc.taskContainers {
		if info == nil || info.Scope != scope {
			continue
		}
		if info.Pending {
			span := info.Span
			if span == (source.Span{}) {
				if sym := tc.symbolFromID(place.Base); sym != nil {
					span = sym.Span
				}
			}
			tc.report(diag.SemaTaskNotAwaited, span, "task container has unconsumed tasks at scope exit")
		}
		delete(tc.taskContainers, place)
	}
}

func (tc *typeChecker) bindingMoved(symID symbols.SymbolID) bool {
	if !symID.IsValid() || tc.movedBindings == nil {
		return false
	}
	_, ok := tc.movedBindings[symID]
	return ok
}
