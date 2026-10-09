package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/types"
)

// A shared borrow of a string literal is the one explicit borrow of an rvalue the
// checker admits (handleBorrow returns before resolvePlace for it,
// borrow_runtime_ops.go:433-435), and typeExplicitBorrow consumes the literal's
// statement-end drop (index_borrow.go:16-17). MIR never materializes it in a frame
// slot: it takes the address of the literal's interned string global
// (mir/lower_expr_ops.go:17-24).
//
// The language names such a value a temporary (LANGUAGE.md §2.11, "Temporary Value
// Promotion") and says nothing about it living past its statement. So its root is a
// Temporary owned by the statement that evaluates the borrow: every use inside that
// statement is proven, and a borrow still held when the statement ends, in a binding,
// an external cell, a container backing or a value leaving by return or break, stays
// a refusal at the borrow itself.
//
// A borrowed string is not a container, so the root is never a storage loan: it
// travels only in values whose type carries a reference, and no loan check (discardLoans,
// localLoan) ever reads it.
const returnOriginTemporaryKeptReason = "borrowed temporary is kept past the statement that owns it"

// statementTemporaryBorrow says whether a shared `&` expression borrows a string
// literal, under parentheses, as a `&string`: exactly the form MIR lowers to the
// address of a string global.
func (b *returnOriginBody) statementTemporaryBorrow(id ast.ExprID, data *ast.ExprUnaryData) bool {
	u := b.function.unit
	if data == nil || data.Op != ast.ExprUnaryRef {
		return false
	}
	in := u.Sema.TypeInterner
	ref, ok := in.Lookup(returnOriginResolveAlias(in, u.Sema.ExprTypes[id]))
	if !ok || ref.Kind != types.KindReference || ref.Mutable || returnOriginResolveAlias(in, ref.Elem) != in.Builtins().String {
		return false
	}
	operand := data.Operand
	for {
		group, grouped := u.Builder.Exprs.Group(operand)
		if !grouped || group == nil {
			break
		}
		operand = group.Inner
	}
	lit, ok := u.Builder.Exprs.Literal(operand)
	return ok && lit != nil && lit.Kind == ast.ExprLitString
}

// refuseRetainedTemporaries closes the temporaries a call's actuals hold when the
// call can keep them with no root to show for it: its value or a formal it may
// write through hides a borrow (hidesBorrow), as a Task holds its async callee's
// borrowed formals and a raw pointer the bytes it was handed.
func (b *returnOriginBody) refuseRetainedTemporaries(callee *returnOriginFunction, value types.TypeID, effects []types.TypeID,
	actuals []returnOriginValue,
) {
	if !b.hidesBorrow(value) && !slices.ContainsFunc(effects, b.hidesBorrow) {
		return
	}
	for i := range actuals {
		if b.sourceBodyConfinesTemporary(callee, i, effects) {
			continue
		}
		actuals[i] = b.closeTemporaries(actuals[i])
	}
}

func (b *returnOriginBody) sourceBodyConfinesTemporary(callee *returnOriginFunction, slot int, effects []types.TypeID) bool {
	if callee == nil || callee.info == nil || !callee.item.Body.IsValid() || slot < 0 || slot >= len(callee.info.Params) {
		return false
	}
	in := callee.unit.Sema.TypeInterner
	formal, ok := in.Lookup(returnOriginResolveAlias(in, callee.info.Params[slot]))
	if !ok || formal.Kind != types.KindReference || formal.Mutable {
		return false
	}
	fact, present := b.analyzer.summaries[callee.key]
	if !present || len(fact.value.callables) != 0 {
		return false
	}
	for _, root := range fact.value.roots {
		if root.kind != returnOriginParam || root.param == uint32(slot) || root.expired {
			return false
		}
	}
	for i, effect := range effects {
		kind, reference := returnOriginFormalBorrowKind(in, effect)
		if reference && kind == BorrowMut && b.hidesBorrow(effect) && !b.analyzer.preservesBorrowedStructSlot(callee, i) {
			return false
		}
	}
	return true
}

// stmt analyzes one statement and then closes the temporaries it owns.
func (b *returnOriginBody) stmt(id ast.StmtID, env returnOriginEnv, targets returnOriginTargets) (returnOriginFlow, error) {
	flow, err := b.stmtKind(id, env, targets)
	if err != nil {
		return returnOriginFlow{}, err
	}
	return b.endStatementTemporaries(flow), nil
}

// endStatementTemporaries refuses every Temporary root that survives its statement,
// on every path out of it, and leaves Unknown in its place. Every earlier statement
// already closed its own, so any root found here was minted by this one.
func (b *returnOriginBody) endStatementTemporaries(flow returnOriginFlow) returnOriginFlow {
	if !flowHoldsTemporary(flow) {
		return flow
	}
	flow = flow.clone()
	flow.normal = b.closeTemporariesIn(flow.normal)
	for key, exit := range flow.exits {
		flow.exits[key] = returnOriginOutcome{env: b.closeTemporariesIn(exit.env), value: b.closeTemporaries(exit.value)}
	}
	return flow
}

func flowHoldsTemporary(flow returnOriginFlow) bool {
	if envHoldsTemporary(flow.normal) {
		return true
	}
	for _, exit := range flow.exits {
		if envHoldsTemporary(exit.env) || holdsTemporary(exit.value) {
			return true
		}
	}
	return false
}

func envHoldsTemporary(env returnOriginEnv) bool {
	for _, binding := range env.bindings {
		if holdsTemporary(binding.value) {
			return true
		}
	}
	for _, cell := range env.cells {
		if holdsTemporary(cell) {
			return true
		}
	}
	for _, backing := range env.backings {
		if holdsTemporary(backing) {
			return true
		}
	}
	return false
}

func holdsTemporary(value returnOriginValue) bool {
	return slices.ContainsFunc(value.roots, func(root returnOrigin) bool { return root.kind == returnOriginTemporary })
}

// closeTemporariesIn rewrites an environment the caller owns (a clone).
func (b *returnOriginBody) closeTemporariesIn(env returnOriginEnv) returnOriginEnv {
	if !env.reachable {
		return env
	}
	for id, binding := range env.bindings {
		binding.value = b.closeTemporaries(binding.value)
		env.bindings[id] = binding
	}
	for slot, cell := range env.cells {
		env.cells[slot] = b.closeTemporaries(cell)
	}
	for slot, backing := range env.backings {
		env.backings[slot] = b.closeTemporaries(backing)
	}
	return env
}

func (b *returnOriginBody) closeTemporaries(value returnOriginValue) returnOriginValue {
	if !value.normal {
		return value
	}
	kept := false
	roots := make([]returnOrigin, 0, len(value.roots))
	for _, root := range value.roots {
		if root.kind != returnOriginTemporary {
			roots = append(roots, root)
			continue
		}
		kept = true
		if node := b.function.unit.Builder.Exprs.Get(root.temp); node != nil {
			b.pending(node.Span, returnOriginTemporaryKeptReason)
		}
	}
	if !kept {
		return value
	}
	out := returnOriginValueOf(append(roots, returnOrigin{kind: returnOriginUnknown})...)
	out.callables = cloneReturnOriginCallables(value.callables)
	return out
}
