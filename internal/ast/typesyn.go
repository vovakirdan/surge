package ast

import "surge/internal/source"

// TypeExprKind enumerates kinds of type expressions.
type TypeExprKind uint8

const (
	// TypeExprInvalid represents an invalid type expression.
	TypeExprInvalid TypeExprKind = iota
	// TypeExprPath represents a path type expression.
	TypeExprPath
	// TypeExprUnary represents a unary type expression.
	TypeExprUnary
	// TypeExprConst represents a const type expression.
	TypeExprConst
	// TypeExprArray represents an array type expression.
	TypeExprArray
	// TypeExprTuple represents a tuple type expression.
	TypeExprTuple
	// TypeExprFn represents a function type expression.
	TypeExprFn
	// TypeExprOptional represents an optional type expression.
	TypeExprOptional
	// TypeExprErrorable represents an errorable type expression.
	TypeExprErrorable
)

// TypeExpr represents a type expression in the AST.
type TypeExpr struct {
	Kind    TypeExprKind
	Span    source.Span
	Payload PayloadID
}

// TypeExprs manages allocation of type expressions.
type TypeExprs struct {
	Arena  *Arena[TypeExpr]
	Paths  *Arena[TypePath]
	Unary  *Arena[TypeUnary]
	Consts *Arena[TypeConst]
	Arrays *Arena[TypeArray]
	Tuples *Arena[TypeTuple]
	Fns    *Arena[TypeFn]
	Opts   *Arena[TypeOptional]
	Errs   *Arena[TypeErrorable]
}

// NewTypeExprs creates a TypeExprs with arenas for type expression nodes and their payloads initialized.
// If capHint is 0, a default capacity of 1<<7 is used. The returned *TypeExprs has Arena, Paths,
// Unary, Arrays, Tuples and Fns arenas allocated with the given capacity hint.
func NewTypeExprs(capHint uint) *TypeExprs {
	if capHint == 0 {
		capHint = 1 << 7
	}
	return &TypeExprs{
		Arena:  NewArena[TypeExpr](capHint),
		Paths:  NewArena[TypePath](capHint),
		Unary:  NewArena[TypeUnary](capHint),
		Consts: NewArena[TypeConst](capHint),
		Arrays: NewArena[TypeArray](capHint),
		Tuples: NewArena[TypeTuple](capHint),
		Fns:    NewArena[TypeFn](capHint),
		Opts:   NewArena[TypeOptional](capHint),
		Errs:   NewArena[TypeErrorable](capHint),
	}
}

func (t *TypeExprs) new(kind TypeExprKind, span source.Span, payload PayloadID) TypeID {
	return TypeID(t.Arena.Allocate(TypeExpr{
		Kind:    kind,
		Span:    span,
		Payload: payload,
	}))
}

// Get returns the type expression with the given ID.
func (t *TypeExprs) Get(id TypeID) *TypeExpr {
	return t.Arena.Get(uint32(id))
}

// NewPath creates a new path type expression.
func (t *TypeExprs) NewPath(span source.Span, segments []TypePathSegment) TypeID {
	payload := t.Paths.Allocate(TypePath{
		Segments: append([]TypePathSegment(nil), segments...),
	})
	return t.new(TypeExprPath, span, PayloadID(payload))
}

// Path returns the path type data for the given TypeID.
func (t *TypeExprs) Path(id TypeID) (*TypePath, bool) {
	typ := t.Get(id)
	if typ == nil || typ.Kind != TypeExprPath {
		return nil, false
	}
	return t.Paths.Get(uint32(typ.Payload)), true
}

// NewUnary creates a new unary type expression.
func (t *TypeExprs) NewUnary(span source.Span, op TypeUnaryOp, inner TypeID) TypeID {
	payload := t.Unary.Allocate(TypeUnary{
		Op:    op,
		Inner: inner,
	})
	return t.new(TypeExprUnary, span, PayloadID(payload))
}

// NewConst creates a new const type expression.
func (t *TypeExprs) NewConst(span source.Span, value source.StringID) TypeID {
	payload := t.Consts.Allocate(TypeConst{Value: value})
	return t.new(TypeExprConst, span, PayloadID(payload))
}

// Const returns the const type data for the given TypeID.
func (t *TypeExprs) Const(id TypeID) (*TypeConst, bool) {
	typ := t.Get(id)
	if typ == nil || typ.Kind != TypeExprConst {
		return nil, false
	}
	return t.Consts.Get(uint32(typ.Payload)), true
}

// UnaryType returns the unary type data for the given TypeID.
func (t *TypeExprs) UnaryType(id TypeID) (*TypeUnary, bool) {
	typ := t.Get(id)
	if typ == nil || typ.Kind != TypeExprUnary {
		return nil, false
	}
	return t.Unary.Get(uint32(typ.Payload)), true
}

// NewArray creates a new array type expression.
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

// Array returns the array type data for the given TypeID.
func (t *TypeExprs) Array(id TypeID) (*TypeArray, bool) {
	typ := t.Get(id)
	if typ == nil || typ.Kind != TypeExprArray {
		return nil, false
	}
	return t.Arrays.Get(uint32(typ.Payload)), true
}

// NewTuple creates a new tuple type expression.
func (t *TypeExprs) NewTuple(span source.Span, elems []TypeID) TypeID {
	payload := t.Tuples.Allocate(TypeTuple{
		Elems: append([]TypeID(nil), elems...),
	})
	return t.new(TypeExprTuple, span, PayloadID(payload))
}

// Tuple returns the tuple type data for the given TypeID.
func (t *TypeExprs) Tuple(id TypeID) (*TypeTuple, bool) {
	typ := t.Get(id)
	if typ == nil || typ.Kind != TypeExprTuple {
		return nil, false
	}
	return t.Tuples.Get(uint32(typ.Payload)), true
}

// NewFn creates a new function type expression.
func (t *TypeExprs) NewFn(span source.Span, params []TypeFnParam, ret TypeID) TypeID {
	payload := t.Fns.Allocate(TypeFn{
		Params: append([]TypeFnParam(nil), params...),
		Return: ret,
	})
	return t.new(TypeExprFn, span, PayloadID(payload))
}

// Fn returns the function type data for the given TypeID.
func (t *TypeExprs) Fn(id TypeID) (*TypeFn, bool) {
	typ := t.Get(id)
	if typ == nil || typ.Kind != TypeExprFn {
		return nil, false
	}
	return t.Fns.Get(uint32(typ.Payload)), true
}

// NewOptional creates a new optional type expression.
func (t *TypeExprs) NewOptional(span source.Span, inner TypeID) TypeID {
	payload := t.Opts.Allocate(TypeOptional{
		Inner: inner,
	})
	return t.new(TypeExprOptional, span, PayloadID(payload))
}

// Optional returns the optional type data for the given TypeID.
func (t *TypeExprs) Optional(id TypeID) (*TypeOptional, bool) {
	typ := t.Get(id)
	if typ == nil || typ.Kind != TypeExprOptional {
		return nil, false
	}
	return t.Opts.Get(uint32(typ.Payload)), true
}

// NewErrorable creates a new errorable type expression.
func (t *TypeExprs) NewErrorable(span source.Span, inner, err TypeID) TypeID {
	payload := t.Errs.Allocate(TypeErrorable{
		Inner: inner,
		Error: err,
	})
	return t.new(TypeExprErrorable, span, PayloadID(payload))
}

// Errorable returns the errorable type data for the given TypeID.
func (t *TypeExprs) Errorable(id TypeID) (*TypeErrorable, bool) {
	typ := t.Get(id)
	if typ == nil || typ.Kind != TypeExprErrorable {
		return nil, false
	}
	return t.Errs.Get(uint32(typ.Payload)), true
}

// TypePath represents a path type expression.
type TypePath struct {
	Segments []TypePathSegment
}

// TypePathSegment represents a segment in a type path.
type TypePathSegment struct {
	Name     source.StringID
	Generics []TypeID
}

// TypeUnary represents a unary type expression (pointer, reference, etc).
type TypeUnary struct {
	Op    TypeUnaryOp
	Inner TypeID
}

// TypeConst represents a const type literal (string).
type TypeConst struct {
	Value source.StringID
}

// TypeUnaryOp enumerates unary type operators.
type TypeUnaryOp uint8

const (
	// TypeUnaryOwn represents an owned pointer (`own`).
	TypeUnaryOwn TypeUnaryOp = iota
	// TypeUnaryRef represents a reference (`&`).
	TypeUnaryRef
	// TypeUnaryRefMut represents a mutable reference (`&mut`).
	TypeUnaryRefMut
	// TypeUnaryPointer represents a pointer type (`*`).
	TypeUnaryPointer
)

// TypeArrayKind distinguishes between slice and sized array.
type TypeArrayKind uint8

const (
	// ArraySlice represents a dynamic array (slice).
	ArraySlice TypeArrayKind = iota // dynamic, growable array `T[]`
	// ArraySized represents a fixed-length array (`T[N]`).
	ArraySized // fixed-length array `T[N]`
)

// TypeArray represents an array type.
type TypeArray struct {
	Elem        TypeID
	Kind        TypeArrayKind
	Length      ExprID // NoExprID when Kind == ArraySlice
	ConstLength uint64
	HasConstLen bool
}

// TypeTuple represents a tuple type.
type TypeTuple struct {
	Elems []TypeID
}

// TypeFn represents a function type.
type TypeFn struct {
	Params []TypeFnParam
	Return TypeID
}

// TypeFnParam represents a parameter in a function type.
type TypeFnParam struct {
	Type     TypeID
	Name     source.StringID
	Variadic bool
}

// TypeOptional represents an optional type (`?`).
type TypeOptional struct {
	Inner TypeID
}

// TypeErrorable represents an errorable type (`!`).
type TypeErrorable struct {
	Inner TypeID
	Error TypeID
}
