package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

// The two reasons a finalized use has no ordinary typed operation behind it.
// A call HIR spells and the AST does not carries one of them.
const (
	returnOriginUseWithoutOperation = "generic use lacks its original typed operation"
	returnOriginUseOtherOperation   = "generic use disagrees with its original typed operation"
)

// defaultInitValue is `let x: T;`, which HIR lowers as `default::<T>()`
// (internal/hir/lower_stmt.go:204-205). A wildcard binds nothing, so its value
// is never read; the Defaultable requirement is what the declaration owes.
func (b *returnOriginBody) defaultInitValue(id ast.StmtID, span source.Span) returnOriginValue {
	u := b.function.unit
	sym := u.Symbols.Table.Symbols.Get(u.stmtSymbols[id])
	if sym == nil {
		return returnOriginValueOf()
	}
	return b.requireDefaultable(returnOriginView(b.function), sym.Type, span)
}

// useWitnessReason is the reason recorded by the unique producing root (for a
// nongeneric caller) or edge (for a template caller) of this use, or "".
func (a *returnOriginAnalyzer) useWitnessReason(use ConcreteInstantiationUse) string {
	authority := a.units[0].authority
	reason, matched := "", 0
	if use.Caller == (InstanceKey{}) {
		for _, root := range authority.InstantiationGraph.Roots() {
			if root.Kind == use.Kind && root.Template == use.CalleeTemplate && root.Witness.Caller == use.CallerTemplate &&
				root.Witness.Site == use.Site {
				reason, matched = root.Witness.Reason, matched+1
			}
		}
	} else {
		for _, edge := range authority.InstantiationGraph.Edges() {
			if edge.Kind == use.Kind && edge.Caller == use.CallerTemplate && edge.Callee == use.CalleeTemplate &&
				edge.Witness.Site == use.Site {
				reason, matched = edge.Witness.Reason, matched+1
			}
		}
	}
	if matched != 1 {
		return ""
	}
	return reason
}

// checkSynthesizedUse answers a finalized use whose call HIR spells and the AST
// does not: SEMA's own `default::<T>()` for a value-less `let` and for a
// conversion's target argument (internal/sema/type_checker_walk.go:472-489,
// implicit_conversion.go:266-275), and the entrypoint's ExitCode call
// (entrypoint_validation.go validateEntrypointReturn), and a magic method selected for an operator
// (type_expr_ops.go:160-166). Identity is already proven by the caller.
func (a *returnOriginAnalyzer) checkSynthesizedUse(fn, caller *returnOriginFunction, use ConcreteInstantiationUse, reason string) (bool, string) {
	if fn == nil || caller == nil || (reason != returnOriginUseWithoutOperation && reason != returnOriginUseOtherOperation) {
		return false, ""
	}
	switch a.useWitnessReason(use) {
	case "default-init":
		if reason != returnOriginUseWithoutOperation {
			return false, ""
		}
	case "conversion-target":
	case "entrypoint callable":
		return a.checkEntrypointExitUse(fn, caller, use, reason)
	case "magic-op":
		return a.checkOperatorUse(fn, caller, use.Site, use.TemplateArgs, reason)
	default:
		return false, ""
	}
	if op, certified := a.coreArrayIntrinsic(fn); !certified || op != returnOriginArrayDefault {
		return false, ""
	}
	return a.checkBackingIntrinsicUse(fn, use)
}

// checkEntrypointExitUse answers the startup `main().__exit_code()` SEMA bound for an
// `@entrypoint` whose result implements the ExitCode contract. The runtime receives
// an int, and the callee is checked by its own generic promise.
func (a *returnOriginAnalyzer) checkEntrypointExitUse(fn, caller *returnOriginFunction, use ConcreteInstantiationUse, reason string) (bool, string) {
	authority := caller.unit.authority
	matches := 0
	for _, binding := range authority.EntrypointCallableBindings {
		if binding.Role == EntrypointReturnExitCode && binding.Entrypoint == use.CallerTemplate && binding.Callee == use.CalleeTemplate &&
			binding.Site == use.Site && slices.Equal(binding.TemplateArgs, use.TemplateArgs) &&
			binding.ExpectedResult == authority.TypeInterner.Builtins().Int {
			matches++
		}
	}
	if matches != 1 || reason != returnOriginUseWithoutOperation || !fn.item.Body.IsValid() {
		return false, ""
	}
	view := returnOriginBoundView(fn, nil, use.TemplateArgs)
	return true, a.checkGenericPromise(fn, &returnOriginSignature{params: fn.info.Params, effects: fn.info.Params,
		result: fn.info.Result, binding: &view}, use)
}

// checkOperatorUse answers the finalized use of a generic magic method SEMA
// selected for a binary operator. The use's site is the operator expression, not a
// call, so the use is that typed operation only when the expression is the one typed
// node there and its own recorded selection resolves to exactly the finalized callee.
// Only the core concatenation, certified by identity, is answered, and only for an
// instance whose result is borrow-free: the caller's own operator walk answers the
// expression itself (return_origin_expr.go binary), on its borrow-free path or with
// its own refusal, and a body-less callee has no body for the use to check. A user overload,
// even one declared on the same receiver, is a different declaration and stays refused.
func (a *returnOriginAnalyzer) checkOperatorUse(fn, caller *returnOriginFunction, site source.Span, args []types.TypeID, reason string) (handled bool, refusal string) {
	if reason != returnOriginUseOtherOperation {
		return false, ""
	}
	u := caller.unit
	expression := ast.NoExprID
	for id, typ := range u.Sema.ExprTypes {
		if node := u.Builder.Exprs.Get(id); node != nil && node.Span == site {
			if expression.IsValid() || typ == types.NoTypeID {
				return false, ""
			}
			expression = id
		}
	}
	data, binary := u.Builder.Exprs.Binary(expression)
	selected, present := u.Sema.MagicBinarySymbols[expression]
	if !expression.IsValid() || !binary || data == nil || !present || magicNameForBinaryOp(data.Op) != fn.name {
		return false, ""
	}
	for _, operand := range []ast.ExprID{data.Left, data.Right} {
		if _, converted := u.Sema.ImplicitConversions[operand]; converted {
			return false, ""
		}
	}
	if chosen, why := a.selectedCallableFunction(u, selected); why != "" || chosen != fn || !a.coreArrayConcat(fn) {
		return false, ""
	}
	if returnOriginBoundView(fn, nil, args).shape(fn.info.Result) != returnOriginRefFree {
		return true, "generic opaque use requires its type-dependent effect transfer"
	}
	return true, ""
}
