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
	Type    types.TypeID
}

type taskContainerLoop struct {
	place       Place
	popCount    int
	popConsumed int
	earlyExit   bool
	popBindings []taskContainerPopBinding
}

type taskContainerPopBinding struct {
	symID    symbols.SymbolID
	span     source.Span
	consumed bool
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

func (tc *typeChecker) isSuspendSafeType(id types.TypeID) bool {
	visited := make(map[types.TypeID]struct{})
	return tc.isSuspendSafeTypeVisited(id, visited)
}

func (tc *typeChecker) isSuspendSafeTypeVisited(id types.TypeID, visited map[types.TypeID]struct{}) bool {
	if id == types.NoTypeID || tc.types == nil {
		return false
	}
	id = tc.valueType(id)
	if id == types.NoTypeID {
		return false
	}
	if _, seen := visited[id]; seen {
		return true
	}
	visited[id] = struct{}{}
	if tc.isTaskType(id) {
		return true
	}
	if elem, _, fixed, ok := tc.arrayInfo(id); ok && !fixed {
		return tc.isSuspendSafeTypeVisited(elem, visited)
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

func (tc *typeChecker) markTaskContainerPending(place Place, span source.Span, containerType types.TypeID) {
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
	if containerType != types.NoTypeID {
		info.Type = containerType
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
					Type:    info.Type,
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
				tc.markTaskContainerPending(dest, span, valueType)
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
	tc.markTaskContainerPending(place, tc.exprSpan(target), containerType)
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
					Type:    info.Type,
				}
				delete(tc.taskContainers, src)
				return
			}
		}
		if tc.builder != nil {
			if expr := tc.builder.Exprs.Get(value); expr != nil {
				switch expr.Kind {
				case ast.ExprArray, ast.ExprStruct:
					tc.markTaskContainerPending(place, span, valueType)
				}
			}
		}
	}
}

func (tc *typeChecker) checkTaskContainerEscape(expr ast.ExprID, exprType types.TypeID, span source.Span) {
	if !expr.IsValid() || !tc.isTaskContainerType(exprType) {
		return
	}
	tc.reportTaskContainerEscape(expr, span)
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
			tc.report(diag.SemaTaskNotAwaited, span, "task container has unconsumed tasks at scope exit (drain required)")
		}
		delete(tc.taskContainers, place)
	}
}

func (tc *typeChecker) scopeActive(scope symbols.ScopeID) bool {
	if !scope.IsValid() {
		return false
	}
	for _, current := range tc.scopeStack {
		if current == scope {
			return true
		}
	}
	return false
}

func (tc *typeChecker) reportTaskContainerEscape(expr ast.ExprID, span source.Span) {
	if expr.IsValid() {
		if place, ok := tc.taskContainerPlace(expr); ok {
			tc.markTaskContainerConsumed(place)
		}
	}
	tc.report(diag.SemaTaskLifetimeError, span, "task container cannot escape its scope")
}

func (tc *typeChecker) checkTaskContainersLiveAcrossAwait(span source.Span) {
	if tc.taskContainers == nil {
		return
	}
	for place, info := range tc.taskContainers {
		if info == nil || !info.Pending {
			continue
		}
		if !tc.scopeActive(info.Scope) {
			continue
		}
		if tc.isSuspendSafeType(info.Type) {
			continue
		}
		if tc.taskContainerLoopAllowsAwait(info) {
			continue
		}
		tc.report(diag.SemaTaskLifetimeError, span, "task container cannot live across await")
		tc.markTaskContainerConsumed(place)
		return
	}
}

func (tc *typeChecker) taskContainerLoopAllowsAwait(info *taskContainerInfo) bool {
	if info == nil || len(tc.taskContainerLoops) == 0 {
		return false
	}
	for i := len(tc.taskContainerLoops) - 1; i >= 0; i-- {
		loop := tc.taskContainerLoops[i]
		if loop.popCount == 0 {
			continue
		}
		if existing := tc.taskContainers[loop.place]; existing == info {
			return true
		}
	}
	return false
}

func (tc *typeChecker) bindingMoved(symID symbols.SymbolID) bool {
	if !symID.IsValid() || tc.movedBindings == nil {
		return false
	}
	_, ok := tc.movedBindings[symID]
	return ok
}

func (tc *typeChecker) enterTaskContainerLoop(place Place) {
	if !place.IsValid() {
		return
	}
	tc.taskContainerLoops = append(tc.taskContainerLoops, taskContainerLoop{place: place})
}

func (tc *typeChecker) leaveTaskContainerLoop() (taskContainerLoop, bool) {
	if len(tc.taskContainerLoops) == 0 {
		return taskContainerLoop{}, false
	}
	idx := len(tc.taskContainerLoops) - 1
	loop := tc.taskContainerLoops[idx]
	tc.taskContainerLoops = tc.taskContainerLoops[:idx]
	return loop, true
}

func (tc *typeChecker) noteTaskContainerPop(place Place) {
	if !place.IsValid() {
		return
	}
	for i := len(tc.taskContainerLoops) - 1; i >= 0; i-- {
		if tc.taskContainerLoops[i].place == place {
			tc.taskContainerLoops[i].popCount++
			return
		}
	}
}

func (tc *typeChecker) noteTaskContainerPopBinding(place Place, symID symbols.SymbolID, span source.Span) {
	if !place.IsValid() || !symID.IsValid() {
		return
	}
	for i := len(tc.taskContainerLoops) - 1; i >= 0; i-- {
		if tc.taskContainerLoops[i].place != place {
			continue
		}
		for _, binding := range tc.taskContainerLoops[i].popBindings {
			if binding.symID == symID {
				return
			}
		}
		tc.taskContainerLoops[i].popBindings = append(tc.taskContainerLoops[i].popBindings, taskContainerPopBinding{
			symID: symID,
			span:  span,
		})
		return
	}
}

func (tc *typeChecker) taskContainerLoopDrained(loop taskContainerLoop) bool {
	if loop.popCount == 0 {
		return false
	}
	return loop.popConsumed >= loop.popCount
}

func (tc *typeChecker) taskContainerPopSource(expr ast.ExprID) (Place, bool) {
	if !expr.IsValid() || tc.builder == nil {
		return Place{}, false
	}
	expr = tc.unwrapGroupExpr(expr)
	if call, ok := tc.builder.Exprs.Call(expr); ok && call != nil {
		if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil {
			if tc.lookupName(member.Field) == "pop" && len(call.Args) == 0 {
				recvType := tc.result.ExprTypes[member.Target]
				if recvType == types.NoTypeID {
					recvType = tc.typeExpr(member.Target)
				}
				if tc.isTaskContainerType(recvType) {
					return tc.taskContainerPlace(member.Target)
				}
			}
			if place, ok := tc.taskContainerPopSource(member.Target); ok {
				return place, true
			}
		} else if place, ok := tc.taskContainerPopSource(call.Target); ok {
			return place, true
		}
	}
	if unary, ok := tc.builder.Exprs.Unary(expr); ok && unary != nil {
		return tc.taskContainerPopSource(unary.Operand)
	}
	if await, ok := tc.builder.Exprs.Await(expr); ok && await != nil {
		return tc.taskContainerPopSource(await.Value)
	}
	return Place{}, false
}

func (tc *typeChecker) trackTaskContainerPopBinding(symID symbols.SymbolID, value ast.ExprID) {
	if !symID.IsValid() || !value.IsValid() {
		return
	}
	place, ok := tc.taskContainerPopSource(value)
	if !ok {
		return
	}
	tc.noteTaskContainerPopBinding(place, symID, tc.exprSpan(value))
}

func (tc *typeChecker) trackTaskContainerPopBindingFromAssign(left, right ast.ExprID) {
	left = tc.unwrapGroupExpr(left)
	expr := tc.builder.Exprs.Get(left)
	if expr == nil || expr.Kind != ast.ExprIdent {
		return
	}
	symID := tc.symbolForExpr(left)
	tc.trackTaskContainerPopBinding(symID, right)
}

func (tc *typeChecker) noteTaskContainerPopBindingConsumed(symID symbols.SymbolID) {
	if !symID.IsValid() {
		return
	}
	for i := len(tc.taskContainerLoops) - 1; i >= 0; i-- {
		loop := &tc.taskContainerLoops[i]
		for idx := range loop.popBindings {
			binding := &loop.popBindings[idx]
			if binding.symID != symID || binding.consumed {
				continue
			}
			binding.consumed = true
			loop.popConsumed++
			return
		}
	}
}

func (tc *typeChecker) noteTaskContainerPopConsumedByExpr(expr ast.ExprID) {
	place, ok := tc.taskContainerPopSource(expr)
	if !ok {
		return
	}
	for i := len(tc.taskContainerLoops) - 1; i >= 0; i-- {
		if tc.taskContainerLoops[i].place == place {
			tc.taskContainerLoops[i].popConsumed++
			return
		}
	}
}

func (tc *typeChecker) noteTaskContainerLoopBreak() {
	if len(tc.taskContainerLoops) == 0 {
		return
	}
	tc.taskContainerLoops[len(tc.taskContainerLoops)-1].earlyExit = true
}

func (tc *typeChecker) noteTaskContainerLoopReturn() {
	for i := range tc.taskContainerLoops {
		tc.taskContainerLoops[i].earlyExit = true
	}
}

func (tc *typeChecker) taskContainerDrainLoop(cond ast.ExprID) (Place, bool) {
	if !cond.IsValid() || tc.builder == nil {
		return Place{}, false
	}
	cond = tc.unwrapGroupExpr(cond)
	bin, ok := tc.builder.Exprs.Binary(cond)
	if !ok || bin == nil {
		return Place{}, false
	}
	if place, ok := tc.taskContainerLenCall(bin.Left); ok {
		if tc.lenNonEmptyComparison(bin.Op, bin.Right, true) {
			return place, true
		}
	}
	if place, ok := tc.taskContainerLenCall(bin.Right); ok {
		if tc.lenNonEmptyComparison(bin.Op, bin.Left, false) {
			return place, true
		}
	}
	return Place{}, false
}

func (tc *typeChecker) taskContainerLenCall(expr ast.ExprID) (Place, bool) {
	if !expr.IsValid() || tc.builder == nil {
		return Place{}, false
	}
	expr = tc.unwrapGroupExpr(expr)
	call, ok := tc.builder.Exprs.Call(expr)
	if !ok || call == nil {
		return Place{}, false
	}
	if len(call.Args) != 0 {
		return Place{}, false
	}
	member, ok := tc.builder.Exprs.Member(call.Target)
	if !ok || member == nil {
		return Place{}, false
	}
	if tc.lookupName(member.Field) != "__len" {
		return Place{}, false
	}
	recvType := tc.typeExpr(member.Target)
	if !tc.isTaskContainerType(recvType) {
		return Place{}, false
	}
	place, ok := tc.taskContainerPlace(member.Target)
	if !ok {
		return Place{}, false
	}
	return place, true
}

func (tc *typeChecker) lenNonEmptyComparison(op ast.ExprBinaryOp, other ast.ExprID, lenOnLeft bool) bool {
	if !lenOnLeft {
		op = swapComparisonOp(op)
	}
	val, ok := tc.literalIntValue(other)
	if !ok {
		return false
	}
	switch op {
	case ast.ExprBinaryNotEq:
		return val == 0
	case ast.ExprBinaryGreater:
		return val == 0
	case ast.ExprBinaryGreaterEq:
		return val == 1
	default:
		return false
	}
}

func swapComparisonOp(op ast.ExprBinaryOp) ast.ExprBinaryOp {
	switch op {
	case ast.ExprBinaryLess:
		return ast.ExprBinaryGreater
	case ast.ExprBinaryLessEq:
		return ast.ExprBinaryGreaterEq
	case ast.ExprBinaryGreater:
		return ast.ExprBinaryLess
	case ast.ExprBinaryGreaterEq:
		return ast.ExprBinaryLessEq
	default:
		return op
	}
}

func (tc *typeChecker) literalIntValue(expr ast.ExprID) (int64, bool) {
	if !expr.IsValid() || tc.builder == nil {
		return 0, false
	}
	expr = tc.unwrapGroupExpr(expr)
	if cast, ok := tc.builder.Exprs.Cast(expr); ok && cast != nil {
		return tc.literalIntValue(cast.Value)
	}
	if unary, ok := tc.builder.Exprs.Unary(expr); ok && unary != nil {
		switch unary.Op {
		case ast.ExprUnaryPlus:
			return tc.literalIntValue(unary.Operand)
		case ast.ExprUnaryMinus:
			if val, ok := tc.literalIntValue(unary.Operand); ok {
				return -val, true
			}
		}
		return 0, false
	}
	lit, ok := tc.builder.Exprs.Literal(expr)
	if !ok || lit == nil {
		return 0, false
	}
	if lit.Kind != ast.ExprLitInt && lit.Kind != ast.ExprLitUint {
		return 0, false
	}
	raw := tc.lookupName(lit.Value)
	val, err := parseIntLiteral(raw)
	if err != nil {
		return 0, false
	}
	return val, true
}
