package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

type returnOriginIndexType struct {
	container types.TypeID
	family    types.TypeID
	element   types.TypeID
	reference bool
}

func (b *returnOriginBody) index(id ast.ExprID, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	data, ok := u.Builder.Exprs.Index(id)
	if !ok || data == nil {
		return b.unknownExpr(env, u.Builder.Exprs.Get(id).Span, "index has no original operands"), nil
	}
	target, err := b.expr(data.Target, env, targets)
	if err != nil || !target.flow.normal.reachable {
		return target, err
	}
	var out returnOriginExprResult
	flow, err := target.flow.then(func(next returnOriginEnv) (returnOriginFlow, error) {
		var stepErr error
		out, stepErr = b.expr(data.Index, next, targets)
		return out.flow, stepErr
	})
	if err != nil {
		return returnOriginExprResult{}, err
	}
	out.flow = flow
	if !flow.normal.reachable {
		return out, nil
	}
	span := u.Builder.Exprs.Get(id).Span
	primitive, reason := b.analyzer.indexOperation(b.function, id)
	out.storage = returnOriginValue{}
	if reason != "" {
		b.pending(span, reason)
		out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		return out, nil
	}
	// Scalar string results discard reference facts only after both operands'
	// effects and exits have passed through the ordinary flow transfer.
	if primitive.family == u.Sema.TypeInterner.Builtins().String {
		out.value = returnOriginValueOf()
		return out, nil
	}
	owner := target.storage
	if primitive.reference {
		owner = target.value
	}
	if !owner.normal || len(owner.roots) == 0 {
		b.pending(span, "indexed element lacks its evaluated storage owner")
		owner = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	b.checkExpired(owner, span)
	out.value, out.storage = owner.clone(), owner.clone()
	return out, nil
}

func returnOriginIndexResolve(in *types.Interner, id types.TypeID) (types.TypeID, types.Type, bool) {
	seen := make(map[types.TypeID]bool)
	for !seen[id] {
		seen[id] = true
		if target, ok := in.AliasTarget(id); ok {
			id = target
			continue
		}
		typ, ok := in.Lookup(id)
		return id, typ, ok
	}
	return types.NoTypeID, types.Type{}, false
}

// Registered declaration identity distinguishes built-in storage from a
// namesake nominal type. Reference-bearing contents do not make owned storage
// an incoming reference; only the actual outer reference kind does that.
func returnOriginIndexContainer(in *types.Interner, id types.TypeID) (returnOriginIndexType, bool) {
	var out returnOriginIndexType
	id, typ, ok := returnOriginIndexResolve(in, id)
	if ok && typ.Kind == types.KindReference {
		out.reference = true
		id, typ, ok = returnOriginIndexResolve(in, typ.Elem)
	}
	if !ok {
		return out, false
	}
	out.container = id
	if id == in.Builtins().String {
		out.family = id
		return out, true
	}
	info, ok := in.StructInfo(id)
	if !ok || info == nil || len(info.TypeArgs) == 0 {
		return out, false
	}
	for _, base := range [...]types.TypeID{in.ArrayNominalType(), in.ArrayFixedNominalType()} {
		original, found := in.StructInfo(base)
		if !found || original == nil || info.Name != original.Name || info.Decl != original.Decl {
			continue
		}
		if (base == in.ArrayNominalType() && len(info.TypeArgs) != 1) ||
			(base == in.ArrayFixedNominalType() && len(info.TypeArgs) > 2) {
			return out, false
		}
		out.family, out.element = base, info.TypeArgs[0]
		_, present := in.Lookup(out.element)
		return out, present
	}
	return out, false
}

func returnOriginTypedIndex(u *returnOriginUnitIndex, id ast.ExprID) (returnOriginIndexType, string) {
	var empty returnOriginIndexType
	data, ok := u.Builder.Exprs.Index(id)
	if !ok || data == nil {
		return empty, "index lacks its original operands"
	}
	in := u.Sema.TypeInterner
	for _, expr := range [...]ast.ExprID{id, data.Target, data.Index} {
		if u.Builder.Exprs.Get(expr) == nil || u.Sema.ExprTypes[expr] == types.NoTypeID {
			return empty, "index lacks its original typed runtime operands"
		}
		if _, converted := u.Sema.ImplicitConversions[expr]; converted {
			return empty, "index requires its implicit conversion effect transfer"
		}
	}
	if u.Sema.ExprTypes[data.Index] != in.Builtins().Int {
		return empty, "index requires a non-scalar index transfer"
	}
	primitive, valid := returnOriginIndexContainer(in, u.Sema.ExprTypes[data.Target])
	if !valid {
		return empty, "index requires its selected container transfer"
	}
	resultID, result, valid := returnOriginIndexResolve(in, u.Sema.ExprTypes[id])
	if primitive.family == in.Builtins().String {
		if !valid || resultID != in.Builtins().Uint32 {
			return empty, "scalar string index has an inconsistent result type"
		}
	} else if !valid || result.Kind != types.KindReference || result.Mutable || result.Elem != primitive.element {
		return empty, "scalar array index has an inconsistent borrowed element type"
	}
	return primitive, ""
}
