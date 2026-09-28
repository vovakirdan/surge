package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A task's result may not contain a task (owner ruling 2026-09-26, SemaTaskPayloadIsTask).
// One handle stands for one structured child; a task that hands back another task's
// handle, directly or inside an Option or other union, a tuple, an array, a map or a
// struct field, would give its caller a child nobody in its scope started. Channel<Task<T>>
// is not a task result: a channel's declaration holds only an opaque word, so the walk
// below never reaches its payload.
//
// The rule is checked once, where the offending type is first written or made:
//   - at a source type, an `async fn` result or an `async`/`blocking` block, all of which
//     construct their Task through resolveNamedType with the site's span. A construction
//     whose argument already holds such a task was refused where that argument was made,
//     and a field type re-resolved while a generic nominal is instantiated is answered at
//     the type that instantiates it;
//   - at a call whose result gets such a task only by substituting type arguments into
//     a generic declaration (instantiation builds the type with no source span).
const taskPayloadIsTaskMessage = "a task's result may not contain a task; await the outer work, then `spawn` the inner work in the caller"

// instantiateNamedType is resolveNamedType's instantiation, checked by this rule and the
// task-in-map rule (task_in_map.go) at its source site. nominalInstantiations marks the field
// types re-resolved inside it, which are not sites.
func (tc *typeChecker) instantiateNamedType(symID symbols.SymbolID, args []types.TypeID, span source.Span) types.TypeID {
	tc.nominalInstantiations++
	instantiated := tc.instantiateType(symID, args, span, "type")
	tc.nominalInstantiations--
	tc.reportTaskPayloadIsTask(instantiated, args, span)
	tc.reportNominalTaskInMap(instantiated, args, span)
	return instantiated
}

// reportTaskPayloadIsTask runs where resolveNamedType built instantiated from args at span.
func (tc *typeChecker) reportTaskPayloadIsTask(instantiated types.TypeID, args []types.TypeID, span source.Span) {
	if span == (source.Span{}) || tc.nominalInstantiations != 0 || !tc.holdsNestedTask(instantiated, make(map[types.TypeID]bool)) {
		return
	}
	for _, arg := range args {
		if tc.holdsNestedTask(arg, make(map[types.TypeID]bool)) {
			return
		}
	}
	tc.report(diag.SemaTaskPayloadIsTask, span, taskPayloadIsTaskMessage)
}

// reportCallTaskPayloadIsTask refuses a call whose result holds a task in a task's result
// when the callee's own declared result does not: the nesting came from its type arguments.
func (tc *typeChecker) reportCallTaskPayloadIsTask(id ast.ExprID, span source.Span, result types.TypeID) {
	if result == types.NoTypeID || !tc.holdsNestedTask(result, make(map[types.TypeID]bool)) {
		return
	}
	sym := tc.symbolFromID(tc.symbolForExpr(id))
	if sym == nil || sym.Type == types.NoTypeID || tc.types == nil {
		return
	}
	info, ok := tc.types.FnInfo(sym.Type)
	if !ok || info == nil || tc.holdsNestedTask(info.Result, make(map[types.TypeID]bool)) {
		return
	}
	tc.report(diag.SemaTaskPayloadIsTask, span, taskPayloadIsTaskMessage)
}

// isCoreTask is the core Task by declaration identity: the runtime-handle family is
// marked only for the builtin core declarations (type_decl_core.go, MarkRuntimeHandleType
// keys it by name and declaration span), and among those families the name picks Task.
func (tc *typeChecker) isCoreTask(id types.TypeID) bool {
	return tc.types != nil && tc.types.IsRuntimeHandleType(id) && tc.isTaskType(id)
}

// holdsNestedTask reports a core Task somewhere in id whose payload holds a task.
func (tc *typeChecker) holdsNestedTask(id types.TypeID, seen map[types.TypeID]bool) bool {
	id = tc.taskWalkType(id)
	if id == types.NoTypeID || seen[id] {
		return false
	}
	seen[id] = true
	if tc.isCoreTask(id) {
		args := tc.types.StructArgs(id)
		return len(args) == 1 && (tc.holdsTask(args[0], make(map[types.TypeID]bool)) || tc.holdsNestedTask(args[0], seen))
	}
	return tc.anyTaskWalkPart(id, func(part types.TypeID) bool { return tc.holdsNestedTask(part, seen) })
}

// holdsTask reports a core Task held by value in id.
func (tc *typeChecker) holdsTask(id types.TypeID, seen map[types.TypeID]bool) bool {
	id = tc.taskWalkType(id)
	if id == types.NoTypeID || seen[id] {
		return false
	}
	seen[id] = true
	if tc.isCoreTask(id) {
		return true
	}
	return tc.anyTaskWalkPart(id, func(part types.TypeID) bool { return tc.holdsTask(part, seen) })
}

// taskWalkType resolves aliases and `own`; a reference or a pointer holds no task by value.
func (tc *typeChecker) taskWalkType(id types.TypeID) types.TypeID {
	if tc.types == nil {
		return types.NoTypeID
	}
	for range 32 {
		id = tc.resolveAlias(id)
		tt, ok := tc.types.Lookup(id)
		if !ok {
			return types.NoTypeID
		}
		if tt.Kind != types.KindOwn {
			return id
		}
		id = tt.Elem
	}
	return types.NoTypeID
}

// anyTaskWalkPart visits what a value of id holds by value: array and map elements,
// struct fields, union members and tag payloads, tuple elements.
func (tc *typeChecker) anyTaskWalkPart(id types.TypeID, visit func(types.TypeID) bool) bool {
	if elem, ok := tc.arrayElemType(id); ok {
		return visit(elem)
	}
	if key, value, ok := tc.types.MapInfo(id); ok {
		return visit(key) || visit(value)
	}
	tt, ok := tc.types.Lookup(id)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindStruct:
		if info, found := tc.types.StructInfo(id); found && info != nil {
			for _, field := range info.Fields {
				if visit(field.Type) {
					return true
				}
			}
		}
	case types.KindUnion:
		if info, found := tc.types.UnionInfo(id); found && info != nil {
			for _, member := range info.Members {
				if member.Type != types.NoTypeID && visit(member.Type) {
					return true
				}
				for _, arg := range member.TagArgs {
					if visit(arg) {
						return true
					}
				}
			}
		}
	case types.KindTuple:
		if info, found := tc.types.TupleInfo(id); found && info != nil {
			for _, elem := range info.Elems {
				if visit(elem) {
					return true
				}
			}
		}
	}
	return false
}
