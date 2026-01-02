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
	ExprTupleIndex
	ExprTernary
	ExprAwait
	ExprTask
	ExprSpawn
	ExprParallel
	ExprSpread
	ExprCompare
	ExprSelect
	ExprRace
	ExprStruct
	ExprAsync
	ExprBlock
	ExprRangeLit
)

type Expr struct {
	Kind    ExprKind
	Span    source.Span
	Payload PayloadID
}

// ExprBinaryOp enumerates binary operator kinds.
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

// String returns the symbol representation of a binary operator.
func (op ExprBinaryOp) String() string {
	switch op {
	case ExprBinaryAdd:
		return "+"
	case ExprBinarySub:
		return "-"
	case ExprBinaryMul:
		return "*"
	case ExprBinaryDiv:
		return "/"
	case ExprBinaryMod:
		return "%"
	case ExprBinaryBitAnd:
		return "&"
	case ExprBinaryBitOr:
		return "|"
	case ExprBinaryBitXor:
		return "^"
	case ExprBinaryShiftLeft:
		return "<<"
	case ExprBinaryShiftRight:
		return ">>"
	case ExprBinaryLogicalAnd:
		return "&&"
	case ExprBinaryLogicalOr:
		return "||"
	case ExprBinaryEq:
		return "=="
	case ExprBinaryNotEq:
		return "!="
	case ExprBinaryLess:
		return "<"
	case ExprBinaryLessEq:
		return "<="
	case ExprBinaryGreater:
		return ">"
	case ExprBinaryGreaterEq:
		return ">="
	case ExprBinaryAssign:
		return "="
	case ExprBinaryAddAssign:
		return "+="
	case ExprBinarySubAssign:
		return "-="
	case ExprBinaryMulAssign:
		return "*="
	case ExprBinaryDivAssign:
		return "/="
	case ExprBinaryModAssign:
		return "%="
	case ExprBinaryBitAndAssign:
		return "&="
	case ExprBinaryBitOrAssign:
		return "|="
	case ExprBinaryBitXorAssign:
		return "^="
	case ExprBinaryShlAssign:
		return "<<="
	case ExprBinaryShrAssign:
		return ">>="
	case ExprBinaryNullCoalescing:
		return "??"
	case ExprBinaryRange:
		return ".."
	case ExprBinaryRangeInclusive:
		return "..="
	case ExprBinaryIs:
		return "is"
	case ExprBinaryHeir:
		return "heir"
	default:
		return "?"
	}
}

// ExprUnaryOp enumerates unary operator kinds.
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

// String returns the symbol representation of a unary operator.
func (op ExprUnaryOp) String() string {
	switch op {
	case ExprUnaryPlus:
		return "+"
	case ExprUnaryMinus:
		return "-"
	case ExprUnaryNot:
		return "!"
	case ExprUnaryDeref:
		return "*"
	case ExprUnaryRef:
		return "&"
	case ExprUnaryRefMut:
		return "&mut"
	case ExprUnaryOwn:
		return "own"
	case ExprUnaryAwait:
		return "await"
	default:
		return "?"
	}
}

// ExprLitKind enumerates literal kinds.
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

// ExprIdentData holds identifier expression details.
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

// CallArg represents a function call argument (positional or named)
type CallArg struct {
	Name  source.StringID // NoStringID for positional args
	Value ExprID
}

type ExprCallData struct {
	Target           ExprID
	Args             []CallArg // Changed from []ExprID to support named args
	TypeArgs         []TypeID
	ArgCommas        []source.Span
	HasTrailingComma bool
}

// HasNamedArgs checks if any argument in the call is named
func (d *ExprCallData) HasNamedArgs() bool {
	for _, arg := range d.Args {
		if arg.Name != source.NoStringID {
			return true
		}
	}
	return false
}

type ExprIndexData struct {
	Target ExprID
	Index  ExprID
}

type ExprMemberData struct {
	Target ExprID
	Field  source.StringID
}

type ExprTupleIndexData struct {
	Target ExprID
	Index  uint32
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

type ExprRangeLitData struct {
	Start     ExprID
	End       ExprID
	Inclusive bool
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

// ExprAsyncData represents an `async { ... }` block expression.
// Body references the block statement containing its statements.
type ExprAsyncData struct {
	Body      StmtID
	AttrStart AttrID
	AttrCount uint32
}

// ExprBlockData represents a block expression `{ stmts; return expr; }`.
// The block must end with a return statement (unless type is nothing).
type ExprBlockData struct {
	Stmts []StmtID
}

// ExprTaskData represents the operand of a `task` expression.
// TODO(sema): enforce async context and Future/Task requirements once sema is in place.
type ExprTaskData struct {
	Value ExprID
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

// ExprTernaryData represents a ternary `cond ? trueExpr : falseExpr` expression.
type ExprTernaryData struct {
	Cond      ExprID
	TrueExpr  ExprID
	FalseExpr ExprID
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

type ExprSelectArm struct {
	Await     ExprID
	Result    ExprID
	IsDefault bool
	Span      source.Span
}

type ExprSelectData struct {
	Arms []ExprSelectArm
}
