package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A Map may not hold a task in its key or its value (owner ruling 2026-09-28, SemaTaskInMap).
// The task container check (task_container.go) drains arrays but does not see into a map,
// and a map has no consuming walk, so a task put into a map can be dropped with the map and
// never run or never be awaited. Task collections live in arrays; a task held behind a
// reference or a pointer, a `far` task handle, or a task behind a channel is not held by the
// map's value.
//
// The rule is checked once, where the offending Map type is first written or made:
//   - at instantiateMapType: a type annotation, a turbofish `Map::<K, V>` and a map literal;
//   - at a generic nominal instantiated with a task (`Box<Task<int>>` over `{ m: Map<int, T> }`),
//     written as a type, inferred by a struct literal or named by a static call
//     (`Box::<Task<int>>::make()`), whose own field types are re-resolved with no site of their own;
//   - at a call whose result gets such a map only by substituting type arguments into a
//     generic declaration, unless an argument or the receiver already holds one.
//
// A map nested in a map is reported at the inner one, as a nested nominal is: the outer site
// sees an argument that already holds such a map. A generic body that builds `Map<int, T>`
// and is instantiated with a task is not a site sema sees: its body is checked once, over T.
const (
	taskInMapMessage = "a Map cannot hold a task: tasks in a map can be lost without being awaited"
	taskInMapHelp    = "keep the tasks in an array and await each one (drain it), or store the task results instead"
)

// taskInMapAnsweredOutside reports an instantiation in progress whose own site answers the rule.
func (tc *typeChecker) taskInMapAnsweredOutside() bool {
	return tc.nominalInstantiations != 0 || tc.taskInMapSiteInstantiations != 0
}

// instantiateAtTaskInMapSite builds a nominal from args at span with build, then answers the
// rule at span: build's own field types are not sites.
func (tc *typeChecker) instantiateAtTaskInMapSite(args []types.TypeID, span source.Span, build func() types.TypeID) types.TypeID {
	tc.taskInMapSiteInstantiations++
	instantiated := build()
	tc.taskInMapSiteInstantiations--
	tc.reportNominalTaskInMap(instantiated, args, span)
	return instantiated
}

// instantiateStaticReceiver is the `Type::<Args>` of a static call, a site of its own.
func (tc *typeChecker) instantiateStaticReceiver(symID symbols.SymbolID, args []types.TypeID, site source.Span) types.TypeID {
	instantiated := tc.instantiateAtTaskInMapSite(args, site, func() types.TypeID {
		return tc.instantiateTypeRejectingChannelPayloadRef(symID, args, site)
	})
	// The receiver keeps its type once refused, so the call does not cascade.
	tc.rejectNominalHoldingRef(symID, instantiated, site)
	return instantiated
}

// reportTaskInMap refuses the Map<key, value> made at span; it answers whether it reported.
func (tc *typeChecker) reportTaskInMap(key, value types.TypeID, span source.Span) bool {
	if span == (source.Span{}) || tc.taskInMapAnsweredOutside() {
		return false
	}
	if !tc.holdsTask(key, make(map[types.TypeID]bool)) && !tc.holdsTask(value, make(map[types.TypeID]bool)) {
		return false
	}
	if tc.holdsTaskMap(key, make(map[types.TypeID]bool)) || tc.holdsTaskMap(value, make(map[types.TypeID]bool)) {
		return true // the inner map was refused where it was made
	}
	tc.emitTaskInMap(span)
	return true
}

// reportNominalTaskInMap runs where resolveNamedType, a struct literal or a static call built
// instantiated from args at span.
func (tc *typeChecker) reportNominalTaskInMap(instantiated types.TypeID, args []types.TypeID, span source.Span) {
	if span == (source.Span{}) || tc.taskInMapAnsweredOutside() || !tc.nominalHoldsTaskMap(instantiated) {
		return
	}
	for _, arg := range args {
		if tc.holdsTaskMap(arg, make(map[types.TypeID]bool)) {
			return
		}
	}
	tc.emitTaskInMap(span)
}

// reportCallTaskInMap refuses a call whose result holds a task in a map when neither the
// callee's declared result, nor an argument, nor the receiver holds one: the map came from
// the call's type arguments.
func (tc *typeChecker) reportCallTaskInMap(id ast.ExprID, span source.Span, call *ast.ExprCallData, result types.TypeID) {
	if result == types.NoTypeID || call == nil || tc.result == nil || !tc.callHoldsTaskMap(result) {
		return
	}
	sym := tc.symbolFromID(tc.symbolForExpr(id))
	if sym == nil || sym.Type == types.NoTypeID || tc.types == nil {
		return
	}
	info, ok := tc.types.FnInfo(sym.Type)
	if !ok || info == nil || tc.callHoldsTaskMap(info.Result) {
		return
	}
	operands := make([]ast.ExprID, 0, len(call.Args)+1)
	if member, isMember := tc.builder.Exprs.Member(call.Target); isMember && member != nil {
		operands = append(operands, member.Target)
	}
	for _, arg := range call.Args {
		operands = append(operands, arg.Value)
	}
	for _, operand := range operands {
		if tc.callHoldsTaskMap(tc.result.ExprTypes[operand]) {
			return
		}
	}
	tc.emitTaskInMap(span)
}

// callHoldsTaskMap is holdsTaskMap, cached: a call's types are complete by the time it is checked.
func (tc *typeChecker) callHoldsTaskMap(id types.TypeID) bool {
	if id == types.NoTypeID {
		return false
	}
	if answer, ok := tc.taskMapCallAnswers[id]; ok {
		return answer
	}
	answer := tc.holdsTaskMap(id, make(map[types.TypeID]bool))
	if tc.taskMapCallAnswers == nil {
		tc.taskMapCallAnswers = make(map[types.TypeID]bool)
	}
	tc.taskMapCallAnswers[id] = answer
	return answer
}

func (tc *typeChecker) emitTaskInMap(span source.Span) {
	if b := diag.ReportError(tc.reporter, diag.SemaTaskInMap, span, taskInMapMessage); b != nil {
		b.WithHelp(span, taskInMapHelp)
		b.Emit()
	}
}

// holdsTaskMap reports a core Map somewhere in id, held by value, whose key or value holds a task.
func (tc *typeChecker) holdsTaskMap(id types.TypeID, seen map[types.TypeID]bool) bool {
	id = tc.taskWalkType(id)
	if id == types.NoTypeID || seen[id] {
		return false
	}
	seen[id] = true
	if key, value, ok := tc.types.MapInfo(id); ok &&
		(tc.holdsTask(key, make(map[types.TypeID]bool)) || tc.holdsTask(value, make(map[types.TypeID]bool))) {
		return true
	}
	return tc.anyTaskWalkPart(id, func(part types.TypeID) bool { return tc.holdsTaskMap(part, seen) })
}
