package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// callChoiceTargets supplies context only when the callable's parameter types
// are known without inspecting its arguments. An overload or generic must still
// infer its arguments first; choosing one candidate here would bias resolution.
func (tc *typeChecker) callChoiceTargets(call *ast.ExprCallData) map[ast.ExprID]types.TypeID {
	ident, ok := tc.builder.Exprs.Ident(call.Target)
	if !ok || ident == nil || len(call.TypeArgs) != 0 {
		return nil
	}
	var choices []int
	for i, arg := range call.Args {
		node := tc.builder.Exprs.Get(tc.unwrapGroupExpr(arg.Value))
		if node != nil && (node.Kind == ast.ExprCompare || node.Kind == ast.ExprTernary) {
			choices = append(choices, i)
		}
	}
	if len(choices) == 0 {
		return nil
	}
	symID := tc.symbolForExpr(call.Target)
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return nil
	}
	targets := make(map[ast.ExprID]types.TypeID, len(choices))
	if sym.Kind == symbols.SymbolLet || sym.Kind == symbols.SymbolParam {
		// A local callable shadows functions of the same name.
		info, found := tc.types.FnInfo(tc.resolveAlias(tc.bindingType(symID)))
		if !found || len(info.Params) != len(call.Args) || call.HasNamedArgs() {
			return nil
		}
		for _, i := range choices {
			targets[call.Args[i].Value] = info.Params[i]
		}
		return targets
	}
	candidates := tc.functionCandidates(ident.Name)
	if len(candidates) == 0 && sym.Kind == symbols.SymbolFunction {
		candidates = []symbols.SymbolID{symID}
	}
	if len(candidates) != 1 {
		return nil
	}
	sym = tc.symbolFromID(candidates[0])
	if sym == nil || sym.Kind != symbols.SymbolFunction || len(sym.TypeParams) != 0 || sym.Signature == nil {
		return nil
	}
	sig := sym.Signature
	for _, i := range choices {
		arg := call.Args[i]
		param := i
		if arg.Name != source.NoStringID {
			param = -1
			for j, name := range sig.ParamNames {
				if name == arg.Name {
					param = j
					break
				}
			}
		} else {
			for j, variadic := range sig.Variadic {
				if variadic && i >= j {
					param = j
					break
				}
			}
		}
		if param < 0 || param >= len(sig.Params) || tc.callAllowsImplicitTo(sym, param) {
			continue
		}
		targets[arg.Value] = tc.typeFromKey(sig.Params[param])
	}
	return targets
}

func (tc *typeChecker) typeCallChoiceArgument(expr ast.ExprID, expected types.TypeID) types.TypeID {
	if expected == types.NoTypeID {
		return tc.typeExpr(expr)
	}
	if target, ok := tc.types.Lookup(tc.resolveAlias(expected)); ok && target.Kind == types.KindReference {
		// The call borrows the whole choice result. Its branches may produce
		// owned values, whose statement-end release belongs to the choice;
		// requiring each branch to be a reference changes that ownership.
		return tc.typeExpr(expr)
	}
	prev := tc.callChoiceExpr
	tc.callChoiceExpr = tc.unwrapGroupExpr(expr)
	ty := tc.typeExprWithExpected(expr, expected)
	tc.callChoiceExpr = prev
	return ty
}

// A choice under a call parameter must obey call conversion rules. Binding
// checks alone would admit __to on an arm even without the callee's @allow_to.
func (tc *typeChecker) ensureChoiceTarget(choice ast.ExprID, expected, actual types.TypeID, branch ast.ExprID) {
	if choice == tc.callChoiceExpr {
		var borrowInfo borrowMatchInfo
		if target, kind, ok := tc.tryTagInjection(actual, expected); ok {
			payload, option := tc.optionPayload(target)
			if !option {
				payload, _, _ = tc.resultPayload(target)
			}
			if _, fits := tc.matchArgument(payload, actual, tc.isLiteralExpr(branch), false, branch, &borrowInfo); fits {
				tc.recordImplicitConversionWithKind(branch, actual, target, kind)
				return
			}
		}
		_, matches := tc.matchArgument(expected, actual, tc.isLiteralExpr(branch), false, branch, &borrowInfo)
		nothingCase := actual == tc.types.Builtins().Nothing && tc.unionHoldsNothing(expected)
		if !matches && !nothingCase {
			if borrowInfo.expr.IsValid() {
				tc.reportBorrowFailure(&borrowInfo)
			} else {
				tc.reportCallArgumentTypeMismatch(expected, actual, branch, false)
			}
			return
		}
	}
	tc.ensureBindingTypeMatch(ast.NoTypeID, expected, actual, branch)
}
