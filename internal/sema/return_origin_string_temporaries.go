package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A string rvalue handed to a shared `&string` formal is admitted by the checker with
// no borrow record: applyParamOwnership returns at canMaterializeForRefString
// (magic_ownership.go:31–33, addressability.go:156–186) before handleBorrow runs, so
// Sema.Borrows holds no FromExpr entry for it. The value sits in a temporary that
// outlives the call (temp_drops.go:307–379, hir/lower_expr.go:88–106,
// mir/lower_temp_drops.go:211–253). When the call can keep nothing, that borrow needs
// no owner root; every other case keeps the implicit-borrow obligation.

// returnOriginCoreDeclaration says whether the callee is a synchronous declaration of the
// core library, by identity: its module path is one the project loader reserves for the
// standard library (driver/module_validation.go:32–49) and its symbol carries the builtin
// mark that loader puts on every symbol of a standard-library module
// (driver/diagnose_modules_stdlib.go:135–143). A name selects nothing. A signature cannot
// show a task started and dropped inside a body, in any callee of that body; until the task
// check publishes that fact per formal, only core callees are believed, and the core's only
// async declarations (core/sync.sg) take no string.
func returnOriginCoreDeclaration(fn *returnOriginFunction) bool {
	if fn == nil || fn.candidate == nil || fn.candidate.Async || !isCoreRuntimeModulePath(fn.candidate.ModulePath) {
		return false
	}
	sym := fn.unit.Symbols.Table.Symbols.Get(fn.symbol)
	return sym != nil && sym.Flags&symbols.SymbolFlagBuiltin != 0
}

// callConfinesBorrows says that nothing this call returns, and nothing it can write
// through, can hold a reference, a storage loan or a hidden borrow: the result is
// reference-free with no loan-carrying and no borrow-hiding part, every effect is
// reference-free and hides none, and no `&mut` actual is a loan sink.
func (b *returnOriginBody) callConfinesBorrows(params, effects []types.TypeID, result types.TypeID) bool {
	in := b.function.unit.Sema.TypeInterner
	if returnOriginTypeShape(in, result, nil) != returnOriginRefFree || b.loanWalk(result, true, b.analyzer.loanCarrier) || b.hidesBorrow(result) {
		return false
	}
	return !returnOriginCallHasUnprovedEffects(in, effects) && !b.loanSinkEffects(params, effects) && !slices.ContainsFunc(effects, b.hidesBorrow)
}

// hidesBorrow says whether id, or a part reached through references, fields, members and
// payloads, can keep an address its type does not show. A raw pointer can: rt_string_ptr
// hands out the bytes of what its `&string` formal points to. A core runtime handle can:
// a Task holds its async callee's formals, borrowed ones included, and its type names only
// the result (type_checker_walk.go:128–131). The interner marks Task, Channel and Range as
// one family and never reads a name (type_decl_core.go:85–100); a Channel's ring holds only
// its typed payload and a Range is already a loan carrier, so taking the family refuses
// nothing the census holds. The shape test reads all of these as reference-free.
func (b *returnOriginBody) hidesBorrow(id types.TypeID) bool {
	in := b.function.unit.Sema.TypeInterner
	seen := make(map[types.TypeID]bool)
	var walk func(types.TypeID) bool
	walk = func(id types.TypeID) bool {
		typ, ok := in.Lookup(id)
		if !ok || typ.Kind == types.KindPointer || in.IsRuntimeHandleType(id) {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		children, _ := returnOriginTypeChildren(b.function, id)
		if c, canonical := returnOriginContainer(in, id); canonical {
			children = append(children, c.element)
		}
		if payloads, handle := in.RuntimeHandlePayloads(id); handle {
			children = append(children, payloads...)
		}
		return slices.ContainsFunc(children, walk)
	}
	return walk(id)
}

// stringTemporary says whether expr is a string rvalue under a shared `&string` formal:
// a literal, a concatenation, a conversion or a call result, under parentheses. None of
// the four resolves to a place (borrow_runtime.go:68–156, default arm), so each is one
// the checker materialized. Every other expression kind keeps its obligation.
func (b *returnOriginBody) stringTemporary(expr ast.ExprID, formal types.TypeID) bool {
	u := b.function.unit
	in := u.Sema.TypeInterner
	text := in.Builtins().String
	ref, ok := in.Lookup(returnOriginResolveAlias(in, formal))
	if !ok || ref.Kind != types.KindReference || ref.Mutable || returnOriginResolveAlias(in, ref.Elem) != text ||
		returnOriginResolveAlias(in, u.Sema.ExprTypes[expr]) != text {
		return false
	}
	for {
		group, grouped := u.Builder.Exprs.Group(expr)
		if !grouped || group == nil {
			break
		}
		expr = group.Inner
	}
	node := u.Builder.Exprs.Get(expr)
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.ExprLit, ast.ExprCast, ast.ExprCall:
		return true
	case ast.ExprBinary:
		data, found := u.Builder.Exprs.Binary(expr)
		return found && data != nil && data.Op == ast.ExprBinaryAdd
	}
	return false
}
