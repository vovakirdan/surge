package ast

import (
	"surge/internal/source"
)

// ExprKind enumerates the different kinds of expressions.
type ExprKind uint8

const (
	// ExprIdent represents an identifier expression.
	ExprIdent ExprKind = iota
	// ExprLit represents a literal expression.
	ExprLit
	// ExprCall represents a function call expression.
	ExprCall
	// ExprBinary represents a binary expression.
	ExprBinary
	// ExprUnary represents a unary expression.
	ExprUnary
	// ExprCast represents a cast expression.
	ExprCast
	// ExprGroup represents a grouped expression.
	ExprGroup
	ExprTuple
	ExprArray
	ExprMap
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

// Expr represents an expression node in the AST.
type Expr struct {
	Kind    ExprKind
	Span    source.Span
	Payload PayloadID
}

// ExprBinaryOp enumerates binary operator kinds.
type ExprBinaryOp uint8

const (
	// Арифметические

	// ExprBinaryAdd represents the addition operator (+).
	ExprBinaryAdd ExprBinaryOp = iota
	// ExprBinarySub represents the subtraction operator (-).
	ExprBinarySub
	// ExprBinaryMul represents the multiplication operator (*).
	ExprBinaryMul
	// ExprBinaryDiv represents the division operator (/).
	ExprBinaryDiv
	// ExprBinaryMod represents the modulo operator (%).
	ExprBinaryMod

	// Битовые

	// ExprBinaryBitAnd represents the bitwise AND operator (&).
	ExprBinaryBitAnd
	// ExprBinaryBitOr represents the bitwise OR operator (|).
	ExprBinaryBitOr
	// ExprBinaryBitXor represents the bitwise XOR operator (^).
	ExprBinaryBitXor
	// ExprBinaryShiftLeft represents the left shift operator (<<).
	ExprBinaryShiftLeft
	ExprBinaryShiftRight

	// Логические

	// ExprBinaryLogicalAnd represents the logical AND operator (&&).
	ExprBinaryLogicalAnd
	ExprBinaryLogicalOr

	// Сравнения

	// ExprBinaryEq represents the equality operator (==).
	ExprBinaryEq
	ExprBinaryNotEq
	ExprBinaryLess
	ExprBinaryLessEq
	ExprBinaryGreater
	ExprBinaryGreaterEq

	// Присваивание

	// ExprBinaryAssign represents the assignment operator (=).
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

	// ExprBinaryNullCoalescing represents the null coalescing operator (??).
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
	// ExprUnaryPlus represents the unary plus operator (+).
	ExprUnaryPlus ExprUnaryOp = iota
	// ExprUnaryMinus represents the unary minus operator (-).
	ExprUnaryMinus
	// ExprUnaryNot represents the logical NOT operator (!).
	ExprUnaryNot
	// ExprUnaryDeref represents the dereference operator (*).
	ExprUnaryDeref
	// ExprUnaryRef represents the reference operator (&).
	ExprUnaryRef
	// ExprUnaryRefMut represents the mutable reference operator (&mut).
	ExprUnaryRefMut
	// ExprUnaryOwn represents the own operator (own).
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
	// ExprLitInt represents an integer literal.
	ExprLitInt ExprLitKind = iota
	// ExprLitUint represents an unsigned integer literal.
	ExprLitUint
	// ExprLitFloat represents a floating-point literal.
	ExprLitFloat
	// ExprLitString represents a string literal.
	ExprLitString
	// ExprLitTrue represents a true boolean literal.
	ExprLitTrue
	// ExprLitFalse represents a false boolean literal.
	ExprLitFalse
	// ExprLitNothing represents a nothing literal.
	ExprLitNothing
)

// ExprIdentData holds identifier expression details.
type ExprIdentData struct {
	Name source.StringID
}

// ExprLiteralData holds literal expression details.
type ExprLiteralData struct {
	Kind  ExprLitKind
	Value source.StringID // сырое значение для sema
}

// ExprBinaryData holds binary operation expression details.
type ExprBinaryData struct {
	Op    ExprBinaryOp
	Left  ExprID
	Right ExprID
}

// ExprUnaryData holds unary operation expression details.
type ExprUnaryData struct {
	Op      ExprUnaryOp
	Operand ExprID
}

// ExprCastData holds cast expression details.
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

// ExprCallData holds function call expression details.
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

// ExprIndexData holds index expression details.
type ExprIndexData struct {
	Target ExprID
	Index  ExprID
}

// ExprMemberData holds member access expression details.
type ExprMemberData struct {
	Target ExprID
	Field  source.StringID
}

// ExprTupleIndexData holds tuple index expression details.
type ExprTupleIndexData struct {
	Target ExprID
	Index  uint32
}

// ExprGroupData holds parenthesized group expression details.
type ExprGroupData struct {
	Inner ExprID
}

// ExprTupleData holds tuple expression details.
type ExprTupleData struct {
	Elements         []ExprID
	ElementCommas    []source.Span
	HasTrailingComma bool
}

// ExprArrayData holds array literal expression details.
type ExprArrayData struct {
	Elements         []ExprID
	ElementCommas    []source.Span
	HasTrailingComma bool
}

// ExprMapEntry represents a key-value pair in a map literal.
type ExprMapEntry struct {
	Key   ExprID
	Value ExprID
}

// ExprMapData holds map literal expression details.
type ExprMapData struct {
	Entries          []ExprMapEntry
	EntryCommas      []source.Span
	HasTrailingComma bool
}

// ExprRangeLitData holds range literal expression details.
type ExprRangeLitData struct {
	Start     ExprID
	End       ExprID
	Inclusive bool
}

// ExprSpreadData holds spread expression details.
type ExprSpreadData struct {
	Value ExprID
}

// ExprStructField represents a field in a struct literal.
type ExprStructField struct {
	Name  source.StringID
	Value ExprID
}

// ExprStructData holds struct literal expression details.
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

// ExprParallelKind distinguishes parallel map and reduce operations.
type ExprParallelKind uint8

const (
	// ExprParallelMap represents a parallel map operation.
	ExprParallelMap ExprParallelKind = iota
	// ExprParallelReduce represents a parallel reduce operation.
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

// ExprCompareArm represents a single arm in a compare expression.
type ExprCompareArm struct {
	Pattern     ExprID
	PatternSpan source.Span
	Guard       ExprID
	Result      ExprID
	IsFinally   bool
}

// ExprCompareData holds compare expression details.
type ExprCompareData struct {
	Value ExprID
	Arms  []ExprCompareArm
}

// ExprSelectArm represents a single arm in a select expression.
type ExprSelectArm struct {
	Await     ExprID
	Result    ExprID
	IsDefault bool
	Span      source.Span
}

// ExprSelectData holds select expression details.
type ExprSelectData struct {
	Arms []ExprSelectArm
}
