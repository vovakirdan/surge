package ast

import (
	"surge/internal/source"
)

type ExprKind uint8

const (
	ExprIdent ExprKind = iota
	ExprLit
	ExprCall
	ExprBinary
	ExprUnary
	ExprCast
	ExprGroup
	ExprTuple
	ExprIndex
	ExprMember
	ExprTernary
	ExprAwait
	ExprSignal
	ExprParallel
	ExprCompare
)

type Expr struct {
	Kind    ExprKind
	Span    source.Span
	Payload PayloadID
}

// Типы бинарных операторов
type ExprBinaryOp uint8

const (
	// Арифметические
	ExprBinaryAdd ExprBinaryOp = iota
	ExprBinarySub
	ExprBinaryMul
	ExprBinaryDiv
	ExprBinaryMod

	// Битовые
	ExprBinaryBitAnd
	ExprBinaryBitOr
	ExprBinaryBitXor
	ExprBinaryShiftLeft
	ExprBinaryShiftRight

	// Логические
	ExprBinaryLogicalAnd
	ExprBinaryLogicalOr

	// Сравнения
	ExprBinaryEq
	ExprBinaryNotEq
	ExprBinaryLess
	ExprBinaryLessEq
	ExprBinaryGreater
	ExprBinaryGreaterEq

	// Присваивание
	ExprBinaryAssign
	ExprBinaryAddAssign
	ExprBinarySubAssign
	ExprBinaryMulAssign
	ExprBinaryDivAssign
	ExprBinaryModAssign
	ExprBinaryBitAndAssign
	ExprBinaryBitOrAssign
	ExprBinaryBitXorAssign
	ExprBinaryShlAssign
	ExprBinaryShrAssign

	// Специальные операторы
	ExprBinaryNullCoalescing // ??
	ExprBinaryRange          // ..
	ExprBinaryRangeInclusive // ..=
	ExprBinaryIs             // is
)

// Типы унарных операторов
type ExprUnaryOp uint8

const (
	ExprUnaryPlus ExprUnaryOp = iota
	ExprUnaryMinus
	ExprUnaryNot
	ExprUnaryDeref
	ExprUnaryRef
	ExprUnaryRefMut
	ExprUnaryOwn
	ExprUnaryAwait
)

// Типы литералов
type ExprLitKind uint8

const (
	ExprLitInt ExprLitKind = iota
	ExprLitUint
	ExprLitFloat
	ExprLitString
	ExprLitTrue
	ExprLitFalse
	ExprLitNothing
)

// Детали выражений
type ExprIdentData struct {
	Name source.StringID
}

type ExprLiteralData struct {
	Kind  ExprLitKind
	Value source.StringID // сырое значение для sema
}

type ExprBinaryData struct {
	Op    ExprBinaryOp
	Left  ExprID
	Right ExprID
}

type ExprUnaryData struct {
	Op      ExprUnaryOp
	Operand ExprID
}

type ExprCastData struct {
	Value ExprID
	Type  TypeID
}

type ExprCallData struct {
	Target ExprID
	Args   []ExprID
}

type ExprIndexData struct {
	Target ExprID
	Index  ExprID
}

type ExprMemberData struct {
	Target ExprID
	Field  source.StringID
}

type ExprGroupData struct {
	Inner ExprID
}

type ExprTupleData struct {
	Elements []ExprID
}

type Exprs struct {
	Arena    *Arena[Expr]
	Idents   *Arena[ExprIdentData]
	Literals *Arena[ExprLiteralData]
	Binaries *Arena[ExprBinaryData]
	Unaries  *Arena[ExprUnaryData]
	Casts    *Arena[ExprCastData]
	Calls    *Arena[ExprCallData]
	Indices  *Arena[ExprIndexData]
	Members  *Arena[ExprMemberData]
	Groups   *Arena[ExprGroupData]
	Tuples   *Arena[ExprTupleData]
}

func NewExprs(capHint uint) *Exprs {
	if capHint == 0 {
		capHint = 1 << 8
	}
	return &Exprs{
		Arena:    NewArena[Expr](capHint),
		Idents:   NewArena[ExprIdentData](capHint),
		Literals: NewArena[ExprLiteralData](capHint),
		Binaries: NewArena[ExprBinaryData](capHint),
		Unaries:  NewArena[ExprUnaryData](capHint),
		Casts:    NewArena[ExprCastData](capHint),
		Calls:    NewArena[ExprCallData](capHint),
		Indices:  NewArena[ExprIndexData](capHint),
		Members:  NewArena[ExprMemberData](capHint),
		Groups:   NewArena[ExprGroupData](capHint),
		Tuples:   NewArena[ExprTupleData](capHint),
	}
}

func (e *Exprs) new(kind ExprKind, span source.Span, payload PayloadID) ExprID {
	return ExprID(e.Arena.Allocate(Expr{
		Kind:    kind,
		Span:    span,
		Payload: payload,
	}))
}

func (e *Exprs) Get(id ExprID) *Expr {
	return e.Arena.Get(uint32(id))
}

func (e *Exprs) NewIdent(span source.Span, name source.StringID) ExprID {
	payload := e.Idents.Allocate(ExprIdentData{Name: name})
	return e.new(ExprIdent, span, PayloadID(payload))
}

func (e *Exprs) Ident(id ExprID) (*ExprIdentData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprIdent {
		return nil, false
	}
	return e.Idents.Get(uint32(expr.Payload)), true
}

func (e *Exprs) NewLiteral(span source.Span, kind ExprLitKind, value source.StringID) ExprID {
	payload := e.Literals.Allocate(ExprLiteralData{Kind: kind, Value: value})
	return e.new(ExprLit, span, PayloadID(payload))
}

func (e *Exprs) Literal(id ExprID) (*ExprLiteralData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprLit {
		return nil, false
	}
	return e.Literals.Get(uint32(expr.Payload)), true
}

func (e *Exprs) NewBinary(span source.Span, op ExprBinaryOp, left, right ExprID) ExprID {
	payload := e.Binaries.Allocate(ExprBinaryData{Op: op, Left: left, Right: right})
	return e.new(ExprBinary, span, PayloadID(payload))
}

func (e *Exprs) Binary(id ExprID) (*ExprBinaryData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprBinary {
		return nil, false
	}
	return e.Binaries.Get(uint32(expr.Payload)), true
}

func (e *Exprs) NewUnary(span source.Span, op ExprUnaryOp, operand ExprID) ExprID {
	payload := e.Unaries.Allocate(ExprUnaryData{Op: op, Operand: operand})
	return e.new(ExprUnary, span, PayloadID(payload))
}

func (e *Exprs) Unary(id ExprID) (*ExprUnaryData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprUnary {
		return nil, false
	}
	return e.Unaries.Get(uint32(expr.Payload)), true
}

func (e *Exprs) NewCast(span source.Span, value ExprID, typ TypeID) ExprID {
	payload := e.Casts.Allocate(ExprCastData{Value: value, Type: typ})
	return e.new(ExprCast, span, PayloadID(payload))
}

func (e *Exprs) Cast(id ExprID) (*ExprCastData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprCast {
		return nil, false
	}
	return e.Casts.Get(uint32(expr.Payload)), true
}

func (e *Exprs) NewCall(span source.Span, target ExprID, args []ExprID) ExprID {
	payload := e.Calls.Allocate(ExprCallData{Target: target, Args: append([]ExprID(nil), args...)})
	return e.new(ExprCall, span, PayloadID(payload))
}

func (e *Exprs) Call(id ExprID) (*ExprCallData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprCall {
		return nil, false
	}
	return e.Calls.Get(uint32(expr.Payload)), true
}

func (e *Exprs) NewIndex(span source.Span, target, index ExprID) ExprID {
	payload := e.Indices.Allocate(ExprIndexData{Target: target, Index: index})
	return e.new(ExprIndex, span, PayloadID(payload))
}

func (e *Exprs) Index(id ExprID) (*ExprIndexData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprIndex {
		return nil, false
	}
	return e.Indices.Get(uint32(expr.Payload)), true
}

func (e *Exprs) NewMember(span source.Span, target ExprID, field source.StringID) ExprID {
	payload := e.Members.Allocate(ExprMemberData{Target: target, Field: field})
	return e.new(ExprMember, span, PayloadID(payload))
}

func (e *Exprs) Member(id ExprID) (*ExprMemberData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprMember {
		return nil, false
	}
	return e.Members.Get(uint32(expr.Payload)), true
}

func (e *Exprs) NewGroup(span source.Span, inner ExprID) ExprID {
	payload := e.Groups.Allocate(ExprGroupData{Inner: inner})
	return e.new(ExprGroup, span, PayloadID(payload))
}

func (e *Exprs) Group(id ExprID) (*ExprGroupData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprGroup {
		return nil, false
	}
	return e.Groups.Get(uint32(expr.Payload)), true
}

func (e *Exprs) NewTuple(span source.Span, elements []ExprID) ExprID {
	payload := e.Tuples.Allocate(ExprTupleData{Elements: append([]ExprID(nil), elements...)})
	return e.new(ExprTuple, span, PayloadID(payload))
}

func (e *Exprs) Tuple(id ExprID) (*ExprTupleData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprTuple {
		return nil, false
	}
	return e.Tuples.Get(uint32(expr.Payload)), true
}
