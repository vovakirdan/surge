package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A reference that is LOADED before the storage it points into is released
// leaves nothing behind: the consumer holds a copied scalar, and the roots the
// reference carried describe a read that has already happened. MIR decides
// where that load goes (lowerValueExpr and loadInsideDropCarryingBlock,
// internal/mir/lower_arm_result_load.go), and it moves the load ahead of the
// drops in exactly two shapes:
//
//   - a compare whose own type is a value, answered by an arm whose result is a
//     reference to that value: each arm is lowered against the compare's
//     result type, so the arm's block loads before the payload it read frees;
//   - a block expression typed `&T` whose last statement is its `ret`, consumed
//     by a `let`, an assignment to a binding, or a function `return` whose type
//     is `T`: those consumers lower their value for the target type, and the
//     block's final `ret` loads before the block's own drops.
//
// Anything else keeps its roots. In particular a call argument is not such a
// consumer (its lowering is not lowerExprForType), an earlier `ret` of the same
// block is not its last statement, and a block nested inside an arm's result is
// dereferenced only after it has run its own drops.
//
// Only the load's own position is answered here. A root that expired before
// the load, inside the expression that produced the reference, was reported
// where its scope closed, and an unresolved or captured root stays.

// loadedScalar answers the scalar a consumer of type target loads into, when
// the target is a Copy value that holds no reference and can keep no loan.
func (b *returnOriginBody) loadedScalar(target types.TypeID) (types.TypeID, bool) {
	u := b.function.unit
	in := u.Sema.TypeInterner
	target = resolveAlias(in, target)
	if target == types.NoTypeID || !b.erasedType(target) || b.holdsLoan(target) || !u.Sema.IsCopyType(target) {
		return types.NoTypeID, false
	}
	if typ, ok := in.Lookup(target); !ok || typ.Kind == types.KindReference {
		return types.NoTypeID, false
	}
	return target, true
}

// referenceTo reports whether id is a shared reference to exactly elem.
func (b *returnOriginBody) referenceTo(id, elem types.TypeID) bool {
	in := b.function.unit.Sema.TypeInterner
	typ, ok := in.Lookup(resolveAlias(in, id))
	return ok && typ.Kind == types.KindReference && !typ.Mutable && resolveAlias(in, typ.Elem) == elem
}

// keepsLoweredShape reports that HIR lowers this expression as itself, with no
// conversion, bool magic, owned-temp or arm-drop wrapper around it: a wrapper
// is what MIR would see instead, and none of them loads early.
func (b *returnOriginBody) keepsLoweredShape(id ast.ExprID) bool {
	s := b.function.unit.Sema
	if _, wrapped := s.ImplicitConversions[id]; wrapped {
		return false
	}
	if _, wrapped := s.BoolSymbols[id]; wrapped {
		return false
	}
	if _, wrapped := s.TempDrops[id]; wrapped {
		return false
	}
	_, wrapped := s.ArmDropsExpr[id]
	return !wrapped
}

func (b *returnOriginBody) ungroup(id ast.ExprID) ast.ExprID {
	for range 64 {
		group, ok := b.function.unit.Builder.Exprs.Group(id)
		if !ok || group == nil {
			return id
		}
		id = group.Inner
	}
	return id
}

// markLoadedResult records that value is consumed as a scalar of type target.
func (b *returnOriginBody) markLoadedResult(value ast.ExprID, target types.TypeID) {
	scalar, ok := b.loadedScalar(target)
	if !value.IsValid() || !ok {
		return
	}
	if b.loadedResults == nil {
		b.loadedResults = make(map[ast.ExprID]types.TypeID)
	}
	b.loadedResults[b.ungroup(value)] = scalar
}

// loadedArmResult drops the roots an arm's reference result carried when the
// compare's own type is the scalar it points at.
func (b *returnOriginBody) loadedArmResult(compare, result ast.ExprID, value returnOriginValue) returnOriginValue {
	u := b.function.unit
	scalar, ok := b.loadedScalar(u.Sema.ExprTypes[compare])
	if !ok || !b.referenceTo(u.Sema.ExprTypes[result], scalar) {
		return value
	}
	for _, id := range [...]ast.ExprID{result, b.ungroup(result)} {
		if _, wrapped := u.Sema.ImplicitConversions[id]; wrapped {
			return value
		}
		if _, wrapped := u.Sema.BoolSymbols[id]; wrapped {
			return value
		}
		if _, wrapped := u.Sema.TempDrops[id]; wrapped {
			return value
		}
	}
	return loadedRoots(value)
}

// loadedBlockTail answers the statement whose `ret` a scalar consumer loads
// inside this block, or NoStmtID when the block is not that shape.
func (b *returnOriginBody) loadedBlockTail(id ast.ExprID, stmts []ast.StmtID) ast.StmtID {
	u := b.function.unit
	scalar, marked := b.loadedResults[id]
	if !marked || len(stmts) == 0 || !b.keepsLoweredShape(id) || !b.referenceTo(u.Sema.ExprTypes[id], scalar) {
		return ast.NoStmtID
	}
	last := stmts[len(stmts)-1]
	node := u.Builder.Stmts.Get(last)
	if node == nil || node.Kind != ast.StmtRet {
		return ast.NoStmtID
	}
	value := u.Builder.Stmts.Ret(last).Expr
	if !value.IsValid() || u.Sema.ExprTypes[value] != u.Sema.ExprTypes[id] || !b.keepsLoweredShape(value) {
		return ast.NoStmtID
	}
	return last
}

// loadedRoots keeps only the roots no load can answer for.
func loadedRoots(value returnOriginValue) returnOriginValue {
	if !value.normal {
		return value
	}
	kept := make([]returnOrigin, 0, len(value.roots))
	for _, root := range value.roots {
		if root.kind == returnOriginUnknown || root.kind == returnOriginCapture {
			kept = append(kept, root)
		}
	}
	out := returnOriginValueOf(kept...)
	out.callables = cloneReturnOriginCallables(value.callables)
	return out
}

// bindingType is the type a binding was given, the one HIR's `let` lowers its
// initializer against.
func (b *returnOriginBody) bindingType(id symbols.SymbolID) types.TypeID {
	u := b.function.unit
	if typ, ok := u.Sema.BindingTypes[id]; ok && typ != types.NoTypeID {
		return typ
	}
	if sym := u.Symbols.Table.Symbols.Get(id); sym != nil {
		return sym.Type
	}
	return types.NoTypeID
}

// loadedBinding drops what a binding of a Copy scalar was handed: storing into
// it copied the value, and nothing reads roots back out of a scalar, so roots
// kept here only re-report a load that already happened at every later scope
// exit. An erased CONTAINER is not such a binding: a map or array of arrays is
// no loan carrier, yet the backing it holds is read back through its storage
// (containerLoans), so its roots stay.
func (b *returnOriginBody) loadedBinding(id symbols.SymbolID, value returnOriginValue) returnOriginValue {
	typ := b.bindingType(id)
	if _, scalar := b.loadedScalar(typ); !scalar {
		return value
	}
	return loadedRoots(value)
}
