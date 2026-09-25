package sema

import "surge/internal/ast"

func (b *returnOriginBody) constructor(id ast.ExprID, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	node := u.Builder.Exprs.Get(id)
	var children []ast.ExprID
	switch node.Kind {
	case ast.ExprArray:
		data, _ := u.Builder.Exprs.Array(id)
		children = data.Elements
	case ast.ExprTuple:
		data, _ := u.Builder.Exprs.Tuple(id)
		children = data.Elements
	case ast.ExprStruct:
		data, _ := u.Builder.Exprs.Struct(id)
		for _, field := range data.Fields {
			children = append(children, field.Value)
		}
	}
	return b.constructorChildren(id, children, env, targets, "")
}

func (b *returnOriginBody) constructorChildren(id ast.ExprID, children []ast.ExprID, env returnOriginEnv, targets returnOriginTargets, reason string) (returnOriginExprResult, error) {
	node := b.function.unit.Builder.Exprs.Get(id)
	out := originExprValue(env, returnOriginValueOf())
	contents := returnOriginValueOf()
	erased := b.shape(id) == returnOriginRefFree
	for _, child := range children {
		next, err := b.expr(child, out.flow.normal, targets)
		if err != nil {
			return returnOriginExprResult{}, err
		}
		if erased && b.shape(child) == returnOriginRefFree {
			b.discardLoans(next.value, node.Span) // G6-i, per child
		}
		out.flow.normal = returnOriginEnv{}
		out.flow = out.flow.join(next.flow)
		if !out.flow.normal.reachable {
			out.value = returnOriginValue{}
			return out, nil
		}
		contents = contents.join(next.value)
	}
	if reason != "" {
		b.pending(node.Span, reason)
		out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		return out, nil
	}
	// An empty canonical array literal holds no element, whatever its element type.
	if c, canonical := returnOriginContainer(b.function.unit.Sema.TypeInterner, b.function.unit.Sema.ExprTypes[id]); canonical && !c.reference &&
		node.Kind == ast.ExprArray && len(children) == 0 {
		out.value = returnOriginValueOf()
		return out, nil
	}
	// Even a reference-free result must retain all child effects and abrupt
	// exits. Its type can discard contents only after evaluating those children.
	shape := b.shape(id)
	// A tag built in its own template keeps every payload once that template's
	// view proves the payload slots may carry references.
	if shape == returnOriginShapeUnknown && node.Kind == ast.ExprCall &&
		returnOriginView(b.function).shape(b.function.unit.Sema.ExprTypes[id]) == returnOriginCarriesRef {
		shape = returnOriginCarriesRef
	}
	// An untyped anonymous record holds exactly its field values, so it keeps them all.
	if shape == returnOriginShapeUnknown && b.anonymousRecordLiteral(id) {
		shape = returnOriginCarriesRef
	}
	switch shape {
	case returnOriginRefFree:
		out.value = returnOriginValueOf()
	case returnOriginCarriesRef:
		out.value = contents
	default:
		b.pending(node.Span, "constructed result needs concrete borrowed-content facts")
		out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	return out, nil
}
