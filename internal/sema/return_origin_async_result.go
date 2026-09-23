package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

// returnOriginFallthroughResult is the type a reachable end of fn's body gives. A function is
// typed as returning its declared result, except an `async fn` (free or a method), which is typed
// as returning Task<P> for its declared P (type_checker_walk.go:128-131,
// extern_method_headers.go:60-66). Its body still gives P: `return v;` returns v, and falling off
// its end gives `nothing`, which sema admits only when P is `nothing` (SEM3051 otherwise). Compared
// with Task<P>, every async fn ending without a `return` read as a result with no proven value.
// An `async { }` block is not a function: only its exit kinds are checked (its exits must be its own
// `ret`), its value is judged by the payload type, and its end is not judged
// (return_origin_task_blocks.go), so it needs nothing here. The wrapper must be the core Task: a
// user type named Task is no runtime handle (handle families are marked only on core
// declarations, type_decl_core.go), so the old comparison stands for it, fail-closed.
func returnOriginFallthroughResult(fn *returnOriginFunction) types.TypeID {
	result := fn.info.Result
	if fn.item.Flags&ast.FnModifierAsync == 0 {
		return result
	}
	in := fn.unit.Sema.TypeInterner
	info, found := in.StructInfo(result)
	if !found || info == nil || !in.IsRuntimeHandleType(result) {
		return result
	}
	if name, _ := fn.unit.Builder.StringsInterner.Lookup(info.Name); name != "Task" {
		return result
	}
	if payloads, handle := in.RuntimeHandlePayloads(result); handle && len(payloads) == 1 {
		return payloads[0]
	}
	return result
}
