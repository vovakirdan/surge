package ast

import "surge/internal/source"

type TypeExprKind uint8

const (
	TypeExprInvalid TypeExprKind = iota
	TypeExprPath
	TypeExprUnary
	TypeExprArray
	TypeExprTuple
	TypeExprFn
)

type TypeExpr struct {
	Kind    TypeExprKind
	Span    source.Span
	Payload PayloadID
}

type TypeExprs struct {
	Arena  *Arena[TypeExpr]
	Paths  *Arena[TypePath]
	Unary  *Arena[TypeUnary]
	Arrays *Arena[TypeArray]
	Tuples *Arena[TypeTuple]
	Fns    *Arena[TypeFn]
}

// NewTypeExprs creates and returns a TypeExprs with its internal arenas preallocated.
// If capHint is 0, a default capacity of 1<<7 is used. The returned value contains
// separate initialized arenas for TypeExpr nodes and each subtype (paths, unary,
// arrays, tuples, and functions).
func NewTypeExprs(capHint uint) *TypeExprs {
	if capHint == 0 {
		capHint = 1 << 7
	}
	return &TypeExprs{
		Arena:  NewArena[TypeExpr](capHint),
		Paths:  NewArena[TypePath](capHint),
		Unary:  NewArena[TypeUnary](capHint),
		Arrays: NewArena[TypeArray](capHint),
		Tuples: NewArena[TypeTuple](capHint),
		Fns:    NewArena[TypeFn](capHint),
	}
}

func (t *TypeExprs) new(kind TypeExprKind, span source.Span, payload PayloadID) TypeID {
	return TypeID(t.Arena.Allocate(TypeExpr{
		Kind:    kind,
		Span:    span,
		Payload: payload,
	}))
}

func (t *TypeExprs) Get(id TypeID) *TypeExpr {
	return t.Arena.Get(uint32(id))
}

func (t *TypeExprs) NewPath(span source.Span, segments []TypePathSegment) TypeID {
	payload := t.Paths.Allocate(TypePath{
		Segments: append([]TypePathSegment(nil), segments...),
	})
	return t.new(TypeExprPath, span, PayloadID(payload))
}

func (t *TypeExprs) Path(id TypeID) (*TypePath, bool) {
	typ := t.Get(id)
	if typ == nil || typ.Kind != TypeExprPath {
		return nil, false
	}
	return t.Paths.Get(uint32(typ.Payload)), true
}

func (t *TypeExprs) NewUnary(span source.Span, op TypeUnaryOp, inner TypeID) TypeID {
	payload := t.Unary.Allocate(TypeUnary{
		Op:    op,
		Inner: inner,
	})
	return t.new(TypeExprUnary, span, PayloadID(payload))
}

func (t *TypeExprs) UnaryType(id TypeID) (*TypeUnary, bool) {
	typ := t.Get(id)
	if typ == nil || typ.Kind != TypeExprUnary {
		return nil, false
	}
	return t.Unary.Get(uint32(typ.Payload)), true
}

func (t *TypeExprs) NewArray(span source.Span, elem TypeID, kind TypeArrayKind, length ExprID, hasConstLen bool, constLen uint64) TypeID {
	payload := t.Arrays.Allocate(TypeArray{
		Elem:        elem,
		Kind:        kind,
		Length:      length,
		HasConstLen: hasConstLen,
		ConstLength: constLen,
	})
	return t.new(TypeExprArray, span, PayloadID(payload))
}

func (t *TypeExprs) Array(id TypeID) (*TypeArray, bool) {
	typ := t.Get(id)
	if typ == nil || typ.Kind != TypeExprArray {
		return nil, false
	}
	return t.Arrays.Get(uint32(typ.Payload)), true
}

func (t *TypeExprs) NewTuple(span source.Span, elems []TypeID) TypeID {
	payload := t.Tuples.Allocate(TypeTuple{
		Elems: append([]TypeID(nil), elems...),
	})
	return t.new(TypeExprTuple, span, PayloadID(payload))
}

func (t *TypeExprs) Tuple(id TypeID) (*TypeTuple, bool) {
	typ := t.Get(id)
	if typ == nil || typ.Kind != TypeExprTuple {
		return nil, false
	}
	return t.Tuples.Get(uint32(typ.Payload)), true
}

func (t *TypeExprs) NewFn(span source.Span, params []TypeFnParam, ret TypeID) TypeID {
	payload := t.Fns.Allocate(TypeFn{
		Params: append([]TypeFnParam(nil), params...),
		Return: ret,
	})
	return t.new(TypeExprFn, span, PayloadID(payload))
}

func (t *TypeExprs) Fn(id TypeID) (*TypeFn, bool) {
	typ := t.Get(id)
	if typ == nil || typ.Kind != TypeExprFn {
		return nil, false
	}
	return t.Fns.Get(uint32(typ.Payload)), true
}

type TypePath struct {
	Segments []TypePathSegment
}

type TypePathSegment struct {
	Name     source.StringID
	Generics []TypeID
}

type TypeUnary struct {
	Op    TypeUnaryOp
	Inner TypeID
}

type TypeUnaryOp uint8

const (
	TypeUnaryOwn TypeUnaryOp = iota
	TypeUnaryRef
	TypeUnaryRefMut
	TypeUnaryPointer
)

type TypeArrayKind uint8

const (
	ArraySlice TypeArrayKind = iota // dynamic, growable array `T[]`
	ArraySized                      // fixed-length array `T[N]`
)

type TypeArray struct {
	Elem        TypeID
	Kind        TypeArrayKind
	Length      ExprID // NoExprID when Kind == ArraySlice
	ConstLength uint64
	HasConstLen bool
}

type TypeTuple struct {
	Elems []TypeID
}

type TypeFn struct {
	Params []TypeFnParam
	Return TypeID
}

type TypeFnParam struct {
	Type     TypeID
	Name     source.StringID
	Variadic bool
}