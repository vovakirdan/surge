package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/symbols"
)

// droppedBinding answers which binding an explicit `@drop` disposes of, and
// whether that drop releases its storage.
//
// The admitted target is an identifier naming a `let` or a parameter declared in
// this function's own file and scope, the same vocabulary the operand read
// already uses (return_origin_expr.go:35-54). Every other target - a projection,
// an index, a call result - keeps its Pending, because the storage it disposes
// of is not one this analysis names.
//
// `releases` asks OwnsHeap, the axis MIR emits its drop on and the one the
// checker itself asks (borrow_runtime_ops.go:622-624). A value that owns no heap
// storage is not freed here: its slot lives until scope exit, where leaveScope
// expires it like any other local, so the environment must not change.
func (b *returnOriginBody) droppedBinding(target ast.ExprID) (symbols.SymbolID, bool, bool) {
	u := b.function.unit
	node := u.Builder.Exprs.Get(target)
	if node == nil || node.Kind != ast.ExprIdent {
		return symbols.NoSymbolID, false, false
	}
	id := u.Symbols.ExprSymbols[target]
	sym := u.Symbols.Table.Symbols.Get(id)
	if sym == nil || (sym.Kind != symbols.SymbolLet && sym.Kind != symbols.SymbolParam) ||
		sym.Decl.ASTFile != u.FileID || !b.within(sym.Scope, b.function.scope) {
		return symbols.NoSymbolID, false, false
	}
	return id, u.Sema.OwnsHeap(u.Sema.ExprTypes[target]), true
}

// expireBinding ends one owner's incarnation: every borrow this environment
// still holds of that binding's storage becomes expired, wherever it is held -
// another binding, an external cell, or a container backing. Loans the dropped
// value itself carries are untouched, because their own owners are still alive.
//
// A later assignment to the same binding mints a fresh, unexpired root, and the
// two stay distinct (return_origin.go:80-86), so a new incarnation cannot revive
// a borrow of the one that was dropped.
func (e returnOriginEnv) expireBinding(id symbols.SymbolID) returnOriginEnv {
	if !e.reachable || !id.IsValid() {
		return e
	}
	out := e.clone()
	for binding, held := range out.bindings {
		held.value = expireReturnOriginOwner(held.value, id)
		out.bindings[binding] = held
	}
	for slot, value := range out.cells {
		out.cells[slot] = expireReturnOriginOwner(value, id)
	}
	for slot, value := range out.backings {
		out.backings[slot] = expireReturnOriginOwner(value, id)
	}
	return out
}

// expireReturnOriginOwner marks the roots that name one local owner's storage,
// and only those. The value keeps its callables and every other root exactly as
// it held them.
func expireReturnOriginOwner(value returnOriginValue, id symbols.SymbolID) returnOriginValue {
	if !value.normal {
		return value
	}
	roots := slices.Clone(value.roots)
	changed := false
	for i := range roots {
		if roots[i].kind == returnOriginLocal && roots[i].binding == id && !roots[i].expired {
			roots[i].expired = true
			changed = true
		}
	}
	if !changed {
		return value
	}
	out := returnOriginValueOf(roots...)
	out.callables = cloneReturnOriginCallables(value.callables)
	return out
}
