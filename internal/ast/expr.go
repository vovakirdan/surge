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
	ExprArray
	ExprIndex
	ExprMember
	ExprTernary
	ExprAwait
	ExprSpawn
	ExprParallel
	ExprSpread
	ExprCompare
	ExprStruct
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
	ExprBinaryHeir           // heir
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
	Value   ExprID
	Type    TypeID
	RawType ExprID
}

type ExprCallData struct {
	Target           ExprID
	Args             []ExprID
	TypeArgs         []TypeID
	ArgCommas        []source.Span
	HasTrailingComma bool
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
	Elements         []ExprID
	ElementCommas    []source.Span
	HasTrailingComma bool
}

type ExprArrayData struct {
	Elements         []ExprID
	ElementCommas    []source.Span
	HasTrailingComma bool
}

type ExprSpreadData struct {
	Value ExprID
}

type ExprStructField struct {
	Name  source.StringID
	Value ExprID
}

type ExprStructData struct {
	Type             TypeID
	Fields           []ExprStructField
	FieldCommas      []source.Span
	HasTrailingComma bool
	Positional       bool
}

// ExprSpawnData represents the operand of a `spawn` expression.
// TODO(sema): enforce async context and Future/Task requirements once sema is in place.
type ExprSpawnData struct {
	Value ExprID
}

type ExprParallelKind uint8

const (
	ExprParallelMap ExprParallelKind = iota
	ExprParallelReduce
)

// ExprParallelData captures common fields for `parallel map` and `parallel reduce`.
// TODO(sema): validate purity attributes and reduction invariants.
type ExprParallelData struct {
	Kind     ExprParallelKind
	Iterable ExprID
	Init     ExprID // only set for reduce
	Args     []ExprID
	Body     ExprID
}

// ExprAwaitData stores the operand of a postfix `.await` expression.
// TODO(sema): ensure `.await` is only used in async contexts and operates on Future-like values.
type ExprAwaitData struct {
	Value ExprID
}

type ExprCompareArm struct {
	Pattern     ExprID
	PatternSpan source.Span
	Guard       ExprID
	Result      ExprID
	IsFinally   bool
}

type ExprCompareData struct {
	Value ExprID
	Arms  []ExprCompareArm
}

type Exprs struct {
	Arena     *Arena[Expr]
	Idents    *Arena[ExprIdentData]
	Literals  *Arena[ExprLiteralData]
	Binaries  *Arena[ExprBinaryData]
	Unaries   *Arena[ExprUnaryData]
	Casts     *Arena[ExprCastData]
	Calls     *Arena[ExprCallData]
	Indices   *Arena[ExprIndexData]
	Members   *Arena[ExprMemberData]
	Awaits    *Arena[ExprAwaitData]
	Groups    *Arena[ExprGroupData]
	Tuples    *Arena[ExprTupleData]
	Arrays    *Arena[ExprArrayData]
	Spreads   *Arena[ExprSpreadData]
	Spawns    *Arena[ExprSpawnData]
	Parallels *Arena[ExprParallelData]
	Compares  *Arena[ExprCompareData]
	Structs   *Arena[ExprStructData]
}

// NewExprs creates a new Exprs with per-kind arenas preallocated using capHint as the initial capacity.
// If capHint is 0, a default capacity of 1<<8 is used; all expression arenas (Expr, Idents, Literals, Binaries, Unaries, Casts, Calls, Indices, Members, Groups, Tuples, Arrays, Spreads, Compares) are initialized.
func NewExprs(capHint uint) *Exprs {
	if capHint == 0 {
		capHint = 1 << 8
	}
	return &Exprs{
		Arena:     NewArena[Expr](capHint),
		Idents:    NewArena[ExprIdentData](capHint),
		Literals:  NewArena[ExprLiteralData](capHint),
		Binaries:  NewArena[ExprBinaryData](capHint),
		Unaries:   NewArena[ExprUnaryData](capHint),
		Casts:     NewArena[ExprCastData](capHint),
		Calls:     NewArena[ExprCallData](capHint),
		Indices:   NewArena[ExprIndexData](capHint),
		Members:   NewArena[ExprMemberData](capHint),
		Awaits:    NewArena[ExprAwaitData](capHint),
		Groups:    NewArena[ExprGroupData](capHint),
		Tuples:    NewArena[ExprTupleData](capHint),
		Arrays:    NewArena[ExprArrayData](capHint),
		Spreads:   NewArena[ExprSpreadData](capHint),
		Spawns:    NewArena[ExprSpawnData](capHint),
		Parallels: NewArena[ExprParallelData](capHint),
		Compares:  NewArena[ExprCompareData](capHint),
		Structs:   NewArena[ExprStructData](capHint),
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

func (e *Exprs) NewCast(span source.Span, value ExprID, typ TypeID, rawType ExprID) ExprID {
	payload := e.Casts.Allocate(ExprCastData{Value: value, Type: typ, RawType: rawType})
	return e.new(ExprCast, span, PayloadID(payload))
}

func (e *Exprs) Cast(id ExprID) (*ExprCastData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprCast {
		return nil, false
	}
	return e.Casts.Get(uint32(expr.Payload)), true
}

func (e *Exprs) NewCall(span source.Span, target ExprID, args []ExprID, typeArgs []TypeID, argCommas []source.Span, trailing bool) ExprID {
	payload := e.Calls.Allocate(ExprCallData{
		Target:           target,
		Args:             append([]ExprID(nil), args...),
		TypeArgs:         append([]TypeID(nil), typeArgs...),
		ArgCommas:        append([]source.Span(nil), argCommas...),
		HasTrailingComma: trailing,
	})
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

func (e *Exprs) NewAwait(span source.Span, value ExprID) ExprID {
	payload := e.Awaits.Allocate(ExprAwaitData{Value: value})
	return e.new(ExprAwait, span, PayloadID(payload))
}

func (e *Exprs) Await(id ExprID) (*ExprAwaitData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprAwait {
		return nil, false
	}
	return e.Awaits.Get(uint32(expr.Payload)), true
}

func (e *Exprs) NewStruct(span source.Span, typ TypeID, fields []ExprStructField, commas []source.Span, trailing, positional bool) ExprID {
	payload := e.Structs.Allocate(ExprStructData{
		Type:             typ,
		Fields:           append([]ExprStructField(nil), fields...),
		FieldCommas:      append([]source.Span(nil), commas...),
		HasTrailingComma: trailing,
		Positional:       positional,
	})
	return e.new(ExprStruct, span, PayloadID(payload))
}

func (e *Exprs) Struct(id ExprID) (*ExprStructData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprStruct {
		return nil, false
	}
	return e.Structs.Get(uint32(expr.Payload)), true
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

func (e *Exprs) NewTuple(span source.Span, elements []ExprID, commas []source.Span, trailing bool) ExprID {
	payload := e.Tuples.Allocate(ExprTupleData{
		Elements:         append([]ExprID(nil), elements...),
		ElementCommas:    append([]source.Span(nil), commas...),
		HasTrailingComma: trailing,
	})
	return e.new(ExprTuple, span, PayloadID(payload))
}

func (e *Exprs) Tuple(id ExprID) (*ExprTupleData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprTuple {
		return nil, false
	}
	return e.Tuples.Get(uint32(expr.Payload)), true
}

func (e *Exprs) NewArray(span source.Span, elements []ExprID, commas []source.Span, trailing bool) ExprID {
	payload := e.Arrays.Allocate(ExprArrayData{
		Elements:         append([]ExprID(nil), elements...),
		ElementCommas:    append([]source.Span(nil), commas...),
		HasTrailingComma: trailing,
	})
	return e.new(ExprArray, span, PayloadID(payload))
}

func (e *Exprs) Array(id ExprID) (*ExprArrayData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprArray {
		return nil, false
	}
	return e.Arrays.Get(uint32(expr.Payload)), true
}

func (e *Exprs) NewSpread(span source.Span, value ExprID) ExprID {
	payload := e.Spreads.Allocate(ExprSpreadData{Value: value})
	return e.new(ExprSpread, span, PayloadID(payload))
}

func (e *Exprs) NewSpawn(span source.Span, value ExprID) ExprID {
	payload := e.Spawns.Allocate(ExprSpawnData{Value: value})
	return e.new(ExprSpawn, span, PayloadID(payload))
}

func (e *Exprs) Spawn(id ExprID) (*ExprSpawnData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprSpawn {
		return nil, false
	}
	return e.Spawns.Get(uint32(expr.Payload)), true
}

func (e *Exprs) NewParallelMap(span source.Span, iterable ExprID, args []ExprID, body ExprID) ExprID {
	payload := e.Parallels.Allocate(ExprParallelData{
		Kind:     ExprParallelMap,
		Iterable: iterable,
		Init:     NoExprID,
		Args:     append([]ExprID(nil), args...),
		Body:     body,
	})
	return e.new(ExprParallel, span, PayloadID(payload))
}

func (e *Exprs) NewParallelReduce(span source.Span, iterable, init ExprID, args []ExprID, body ExprID) ExprID {
	payload := e.Parallels.Allocate(ExprParallelData{
		Kind:     ExprParallelReduce,
		Iterable: iterable,
		Init:     init,
		Args:     append([]ExprID(nil), args...),
		Body:     body,
	})
	return e.new(ExprParallel, span, PayloadID(payload))
}

func (e *Exprs) Parallel(id ExprID) (*ExprParallelData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprParallel {
		return nil, false
	}
	return e.Parallels.Get(uint32(expr.Payload)), true
}

func (e *Exprs) Spread(id ExprID) (*ExprSpreadData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprSpread {
		return nil, false
	}
	return e.Spreads.Get(uint32(expr.Payload)), true
}

func (e *Exprs) NewCompare(span source.Span, value ExprID, arms []ExprCompareArm) ExprID {
	payload := e.Compares.Allocate(ExprCompareData{
		Value: value,
		Arms:  append([]ExprCompareArm(nil), arms...),
	})
	return e.new(ExprCompare, span, PayloadID(payload))
}

func (e *Exprs) Compare(id ExprID) (*ExprCompareData, bool) {
	expr := e.Get(id)
	if expr == nil || expr.Kind != ExprCompare || !expr.Payload.IsValid() {
		return nil, false
	}
	return e.Compares.Get(uint32(expr.Payload)), true
}
