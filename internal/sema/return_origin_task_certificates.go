package sema

import (
	"slices"

	"surge/internal/types"
)

// coreTaskType answers the core Task family by declaration identity: the result of
// the one core `checkpoint` whose task certificate holds (returnOriginTaskLeaf pins
// the declaration beside it, its single `__opaque` word and its runtime-handle mark),
// as intRangeType answers the Range family from its core constructor. A type is then
// a core Task by that declaration, never by its spelling.
func (a *returnOriginAnalyzer) coreTaskType() types.TypeID {
	var selected *returnOriginFunction
	for _, fn := range a.declarations {
		if fn.name != "checkpoint" || fn.canonicalSourceKey != "builtin" || fn.unit.SourceKey != "core/intrinsics.sg" {
			continue
		}
		if selected != nil {
			return types.NoTypeID
		}
		selected = fn
	}
	if selected == nil || selected.info == nil {
		return types.NoTypeID
	}
	if _, certified := returnOriginTaskHandleResidual(selected, selected.info.Result); !certified {
		return types.NoTypeID
	}
	return selected.info.Result
}

// isCoreTask reports that id is an instantiation of the core Task declaration:
// the same nominal and declaration span as the certified family, one type
// argument, and the runtime-handle mark. An alias or a same-named user type is
// not one, and is left to the refusal it already has.
func (a *returnOriginAnalyzer) isCoreTask(in *types.Interner, id types.TypeID) bool {
	if a == nil || a.taskType == types.NoTypeID || id == types.NoTypeID {
		return false
	}
	family, known := in.StructInfo(a.taskType)
	info, ok := in.StructInfo(id)
	return known && ok && family != nil && info != nil && info.Name == family.Name && info.Decl == family.Decl &&
		len(info.TypeArgs) == 1 && in.IsRuntimeHandleType(id) && !in.IsRefCountedHandle(id)
}

// A core task constructor below returns a handle naming a runtime task, never an
// address inside an input. checkpoint and sleep register a new task that captures
// nothing but a copied delay (rt_async_task.c:509-569; vm/intrinsic_async.go:64-147).
// clone returns the receiver's own task with one more reference, not its slot
// (rt_async_task.c:490-507; vm/intrinsic_task_control.go:9-47;
// backend/llvm/emit_intrinsics_clone.go:76-92). Only the retained core declaration
// with exactly this signature and result structure is believed, and the payload is
// still walked. What a running task borrows, including what a clone shares with its
// original's spawn, is the task check's question (owner ruling 2026-09-15).

// returnOriginTaskHandleResidual answers which part of a certified task constructor's
// result still needs the ordinary walk: the task's payload type.
func returnOriginTaskHandleResidual(fn *returnOriginFunction, result types.TypeID) ([]types.TypeID, bool) {
	if fn == nil || fn.info == nil || fn.candidate == nil || result != fn.info.Result {
		return nil, false
	}
	in := fn.unit.Sema.TypeInterner
	switch fn.name {
	case "checkpoint", "sleep":
		var kinds []returnOriginFreshParam
		if fn.name == "sleep" {
			kinds = []returnOriginFreshParam{freshUint}
		}
		if !returnOriginCoreIntrinsic(fn, 0, 0) || fn.candidate.HasSelf || fn.candidate.ReceiverType != types.NoTypeID ||
			!returnOriginFreshParamsMatch(fn, kinds) || !returnOriginTaskLeaf(fn, result) {
			return nil, false
		}
		info, _ := in.StructInfo(result)
		return slices.Clone(info.TypeArgs), info.TypeArgs[0] == in.Builtins().Nothing
	case "clone":
		receiver := fn.candidate.ReceiverType
		if !returnOriginCoreIntrinsic(fn, 1, 1) || !fn.candidate.HasSelf || len(fn.info.Params) != 1 || result != receiver ||
			!returnOriginTaskLeaf(fn, receiver) {
			return nil, false
		}
		self, typed := in.Lookup(fn.info.Params[0])
		info, _ := in.StructInfo(receiver)
		if !typed || self.Kind != types.KindReference || self.Mutable || self.Elem != receiver ||
			!slices.Equal(info.TypeArgs, fn.candidate.TemplateParams) {
			return nil, false
		}
		return slices.Clone(info.TypeArgs), true
	}
	return nil, false
}

// The leaf is the core Task declared beside fn: one type argument and one `__opaque`
// int64 word, a runtime handle whose object the runtime does not reference-count.
func returnOriginTaskLeaf(fn *returnOriginFunction, id types.TypeID) bool {
	in := fn.unit.Sema.TypeInterner
	info, found := in.StructInfo(id)
	if !found || info == nil || info.Decl.File != fn.item.NameSpan.File || len(info.TypeArgs) != 1 || len(info.Fields) != 1 ||
		!in.IsRuntimeHandleType(id) || in.IsRefCountedHandle(id) {
		return false
	}
	name, _ := fn.unit.Builder.StringsInterner.Lookup(info.Name)
	field, _ := fn.unit.Builder.StringsInterner.Lookup(info.Fields[0].Name)
	return name == "Task" && field == "__opaque" && info.Fields[0].Type == in.Builtins().Int64
}
