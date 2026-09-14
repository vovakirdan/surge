package sema

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
)

func (b *returnOriginBody) compareExpr(id ast.ExprID, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	cmp, _ := u.Builder.Exprs.Compare(id)
	subject, err := b.expr(cmp.Value, env, targets)
	if err != nil || !subject.flow.normal.reachable {
		return subject, err
	}
	remaining := subject.flow.normal
	choices := b.compareChoices(u.Sema.ExprTypes[cmp.Value])
	if b.literalBool(cmp.Value, true) {
		choices = []returnOriginCompareChoice{{literal: ast.ExprLitTrue}}
	} else if b.literalBool(cmp.Value, false) {
		choices = []returnOriginCompareChoice{{literal: ast.ExprLitFalse}}
	}
	out := returnOriginExprResult{flow: subject.flow.clone()}
	out.flow.normal = returnOriginEnv{}
	for _, arm := range cmp.Arms {
		if !remaining.reachable {
			break
		}
		scope, err := b.compareArmScope(id, arm.PatternSpan)
		if err != nil {
			return returnOriginExprResult{}, err
		}
		pattern := returnOriginComparePattern{known: true, all: true}
		if !arm.IsFinally {
			pattern, err = b.readComparePattern(arm.Pattern, scope)
			if err != nil {
				return returnOriginExprResult{}, err
			}
		}
		if !pattern.known {
			b.pending(arm.PatternSpan, "compare pattern needs a precise matching transfer")
		}
		armTargets := targets
		armTargets.scope = scope
		// Runtime pattern operands run before either matching continuation.
		attempt := returnOriginFlow{normal: remaining}
		for _, runtime := range pattern.runtime {
			attempt, err = attempt.then(func(next returnOriginEnv) (returnOriginFlow, error) {
				value, childErr := b.expr(runtime, next, armTargets)
				return value.flow, childErr
			})
			if err != nil {
				return returnOriginExprResult{}, err
			}
		}
		remaining = attempt.normal
		attempt.normal = returnOriginEnv{}
		out.flow = out.flow.join(b.closeFlow(attempt, scope, arm.PatternSpan))
		matched, missed := pattern.split(choices)
		if len(matched) == 0 || !remaining.reachable {
			continue
		}
		entry := b.bindCompareOrigins(pattern.bindings, subject.value, remaining)
		nextArm := returnOriginEnv{}
		if len(missed) != 0 {
			// A pattern mismatch bypasses the guard and all its effects.
			nextArm = remaining.clone()
		}
		if arm.Guard.IsValid() {
			guard, guardErr := b.expr(arm.Guard, entry, armTargets)
			if guardErr != nil {
				return returnOriginExprResult{}, guardErr
			}
			entry = guard.flow.normal
			guard.flow.normal = returnOriginEnv{}
			out.flow = out.flow.join(b.closeFlow(guard.flow, scope, u.Builder.Exprs.Get(arm.Guard).Span))
			if !b.literalBool(arm.Guard, true) {
				closed := b.closeOutcome(returnOriginOutcome{env: entry, value: returnOriginValueOf()}, scope, u.Builder.Exprs.Get(arm.Guard).Span)
				nextArm = nextArm.join(closed.env)
			}
			if b.literalBool(arm.Guard, false) {
				entry = returnOriginEnv{}
			}
		}
		result, resultErr := b.expr(arm.Result, entry, armTargets)
		if resultErr != nil {
			return returnOriginExprResult{}, resultErr
		}
		// closeFlow's normal edge has no result; close the actual value first.
		closed := b.closeOutcome(returnOriginOutcome{env: result.flow.normal, value: result.value}, scope, u.Builder.Exprs.Get(arm.Result).Span)
		result.flow.normal = returnOriginEnv{}
		result.flow = b.closeFlow(result.flow, scope, arm.PatternSpan)
		result.flow.normal = closed.env
		out.flow = out.flow.join(result.flow)
		out.value = out.value.join(closed.value)
		if !arm.Guard.IsValid() || b.literalBool(arm.Guard, true) {
			choices = missed
		}
		remaining = nextArm
	}
	if remaining.reachable {
		b.pending(u.Builder.Exprs.Get(id).Span, "compare has an unproved unmatched continuation")
		out.flow.normal = out.flow.normal.join(remaining)
		out.value = out.value.join(returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}))
	}
	return out, nil
}

func (b *returnOriginBody) compareArmScope(id ast.ExprID, span source.Span) (symbols.ScopeID, error) {
	u := b.function.unit
	var found symbols.ScopeID
	for i, scope := range u.Symbols.Table.Scopes.Data() {
		owner := scope.Owner
		if owner.ASTFile != u.FileID || owner.SourceFile != span.File || owner.Kind != symbols.ScopeOwnerExpr || owner.Expr != id || scope.Span != span {
			continue
		}
		if found.IsValid() {
			return symbols.NoScopeID, fmt.Errorf("return origins: compare %d has an ambiguous arm scope at %v", id, span)
		}
		index, err := safecast.Conv[uint32](i + 1)
		if err != nil {
			return symbols.NoScopeID, err
		}
		found = symbols.ScopeID(index)
	}
	if !found.IsValid() || !b.within(found, b.function.scope) {
		return symbols.NoScopeID, fmt.Errorf("return origins: compare %d has no original arm scope at %v", id, span)
	}
	return found, nil
}
