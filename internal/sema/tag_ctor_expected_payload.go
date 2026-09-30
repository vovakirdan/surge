package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// tagCallPayloadTypes answers, for a tag constructor call written where a
// union is expected, the payload type each argument lands in: `Some([6, 7])`
// returned as an `Option<int[]>` builds an `int[]`, not the `int[2]` the
// literal would be on its own.
//
// Typed on its own, that literal made the call a `Some<int[2]>`, and the
// widening to `Option<int[]>` it then needed moves a fixed array's inline
// bytes into the slot of a dynamic array's handle. Neither backend has that
// conversion (RV2 blocker C). Typing the argument against the payload builds
// the value the union holds in the first place, so there is nothing to widen.
//
// It answers nil unless the call is an unqualified tag constructor with
// positional arguments and no explicit type arguments, the expected type is a
// union holding a tag of that name, and that tag is the only candidate.
func (tc *typeChecker) tagCallPayloadTypes(callID ast.ExprID, call *ast.ExprCallData) []types.TypeID {
	if tc == nil || tc.types == nil || tc.builder == nil || call == nil || len(call.TypeArgs) != 0 || len(call.Args) == 0 {
		return nil
	}
	for _, arg := range call.Args {
		if arg.Name != source.NoStringID {
			return nil
		}
	}
	ident, ok := tc.builder.Exprs.Ident(call.Target)
	if !ok || ident == nil {
		return nil
	}
	member, ok := tc.expectedTagMember(tc.expectedTypeForExpr(callID), tc.lookupName(ident.Name))
	if !ok {
		return nil
	}
	// The tag must be the only candidate: a function of the same name could
	// win the overload, and its parameters are not the payload's.
	candidates := tc.functionCandidates(ident.Name)
	if len(candidates) != 1 {
		return nil
	}
	tag := tc.symbolFromID(candidates[0])
	if tag == nil || tag.Kind != symbols.SymbolTag || tag.Signature == nil || len(tag.Signature.Params) != len(call.Args) {
		return nil
	}
	for _, variadic := range tag.Signature.Variadic {
		if variadic {
			return nil
		}
	}
	// A generic tag's member names its type arguments (`Some(T)` in
	// `Option<T>`); a plain tag's payload is already concrete in its signature.
	names, paramSet := tc.typeParamNameSet(tag)
	if len(names) != len(tag.TypeParams) || (len(names) > 0 && len(names) != len(member.TagArgs)) {
		return nil
	}
	bindings := make(map[string]types.TypeID, len(names))
	for i, name := range names {
		bindings[name] = member.TagArgs[i]
	}
	out := make([]types.TypeID, len(call.Args))
	for i, key := range tag.Signature.Params {
		out[i] = tc.instantiateResultType(key, bindings, paramSet)
	}
	return out
}

// expectedTagMember finds the tag of the given name among the members of an
// expected union.
func (tc *typeChecker) expectedTagMember(expected types.TypeID, tagName string) (types.UnionMember, bool) {
	if expected == types.NoTypeID || tagName == "" {
		return types.UnionMember{}, false
	}
	info, ok := tc.types.UnionInfo(tc.resolveAlias(expected))
	if !ok || info == nil {
		return types.UnionMember{}, false
	}
	for _, member := range info.Members {
		if member.Kind == types.UnionMemberTag && tc.lookupName(member.TagName) == tagName {
			return member, true
		}
	}
	return types.UnionMember{}, false
}

// typeTagPayloadArg types one argument of a tag constructor against the
// payload it lands in. Only the two forms whose own type depends on that are
// given it: an array literal, which becomes the payload's dynamic array, and
// a nested tag constructor, which passes the payload on as its own expected
// union (`Some(Some([6, 7]))` as an `Option<Option<int[]>>`). Every other
// argument is typed exactly as before.
func (tc *typeChecker) typeTagPayloadArg(arg ast.ExprID, payload types.TypeID) types.TypeID {
	if payload == types.NoTypeID {
		return tc.typeExpr(arg)
	}
	if _, isCall := tc.builder.Exprs.Call(tc.unwrapGroups(arg)); isCall {
		return tc.typeTagCallUnder(arg, payload)
	}
	if _, isArray := tc.arrayLiteralInfo(arg); !isArray {
		return tc.typeExpr(arg)
	}
	ty := tc.typeExpr(arg)
	if _, _, fixed, ok := tc.arrayInfo(payload); !ok || fixed {
		return ty
	}
	// Only a literal the payload already accepted is retyped: its elements
	// must be assignable as they stand. A tag constructor is a call, and a call
	// argument takes no implicit `__to` without `@allow_to` (LANGUAGE §6.6.1),
	// so `Some(["a"])` as an `Option<int[]>` stays the mismatch it was.
	if ty != types.NoTypeID && !tc.typesAssignable(payload, ty, true) {
		return ty
	}
	if applied, ok := tc.materializeArrayLiteral(arg, payload); applied && ok {
		return payload
	}
	return ty
}

// typeTagCallUnder types an expression that may be a tag constructor under an
// expected union: a constructor of one of the union's tags is handed the union
// so its payload is typed against it; anything else is typed as it always was.
// A ternary's branches go through here, so `c ? Some([1]) : nothing` returned
// as an `Option<int[]>` builds the same payload a plain `return Some([1])` does.
func (tc *typeChecker) typeTagCallUnder(expr ast.ExprID, expected types.TypeID) types.TypeID {
	if expected == types.NoTypeID || !expr.IsValid() || tc.builder == nil {
		return tc.typeExpr(expr)
	}
	call, ok := tc.builder.Exprs.Call(tc.unwrapGroups(expr))
	if !ok || call == nil {
		return tc.typeExpr(expr)
	}
	ident, ok := tc.builder.Exprs.Ident(call.Target)
	if !ok || ident == nil {
		return tc.typeExpr(expr)
	}
	if _, isMember := tc.expectedTagMember(expected, tc.lookupName(ident.Name)); !isMember {
		return tc.typeExpr(expr)
	}
	return tc.typeExprWithExpected(expr, expected)
}
