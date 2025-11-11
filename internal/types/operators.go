package types

import "surge/internal/ast"

// FamilyMask describes broad categories of types an operator accepts.
type FamilyMask uint32

const (
	FamilyNone FamilyMask = 0
	FamilyAny  FamilyMask = 1 << iota
	FamilyBool
	FamilySignedInt
	FamilyUnsignedInt
	FamilyFloat
	FamilyString
	FamilyArray
	FamilyPointer
	FamilyReference
	FamilyOptional
	FamilyResult
)

const (
	FamilyIntegral = FamilySignedInt | FamilyUnsignedInt
	FamilyNumeric  = FamilyIntegral | FamilyFloat
)

// BinaryResult describes how to derive the result type for an operator.
type BinaryResult uint8

const (
	BinaryResultUnknown BinaryResult = iota
	BinaryResultLeft
	BinaryResultRight
	BinaryResultBool
	BinaryResultNumeric
	BinaryResultRange
)

// BinaryFlags annotate special handling for binary operators.
type BinaryFlags uint16

const (
	BinaryFlagNone       BinaryFlags = 0
	BinaryFlagAssignment BinaryFlags = 1 << iota
	BinaryFlagShortCircuit
	BinaryFlagCommutative
	BinaryFlagSameFamily
	BinaryFlagTypeOperand // right-hand side encodes a type (is/heir)
)

// BinarySpec lists operand families and expected result for an operation.
type BinarySpec struct {
	Left   FamilyMask
	Right  FamilyMask
	Result BinaryResult
	Flags  BinaryFlags
}

// UnaryResult indicates how to derive the resulting type.
type UnaryResult uint8

const (
	UnaryResultUnknown UnaryResult = iota
	UnaryResultSame
	UnaryResultBool
	UnaryResultNumeric
	UnaryResultReference // &expr
	UnaryResultDeref     // *expr
	UnaryResultAwait     // await expr
)

// UnaryFlags capture operator-specific metadata.
type UnaryFlags uint8

const (
	UnaryFlagNone                UnaryFlags = 0
	UnaryFlagRequiresAddressable UnaryFlags = 1 << iota
)

// UnarySpec describes operand expectations for unary operators.
type UnarySpec struct {
	Operand FamilyMask
	Result  UnaryResult
	Flags   UnaryFlags
}

var binarySpecTable = map[ast.ExprBinaryOp][]BinarySpec{
	ast.ExprBinaryAdd: {
		{Left: FamilyNumeric, Right: FamilyNumeric, Result: BinaryResultNumeric, Flags: BinaryFlagCommutative},
		{Left: FamilyString, Right: FamilyString, Result: BinaryResultLeft, Flags: BinaryFlagCommutative | BinaryFlagSameFamily},
		{Left: FamilyArray, Right: FamilyArray, Result: BinaryResultLeft, Flags: BinaryFlagCommutative | BinaryFlagSameFamily},
	},
	ast.ExprBinarySub: {
		{Left: FamilyNumeric, Right: FamilyNumeric, Result: BinaryResultNumeric},
	},
	ast.ExprBinaryMul: {
		{Left: FamilyNumeric, Right: FamilyNumeric, Result: BinaryResultNumeric},
		{Left: FamilyString, Right: FamilyIntegral, Result: BinaryResultLeft},
	},
	ast.ExprBinaryDiv: {
		{Left: FamilyNumeric, Right: FamilyNumeric, Result: BinaryResultNumeric},
	},
	ast.ExprBinaryMod: {
		{Left: FamilyIntegral, Right: FamilyIntegral, Result: BinaryResultNumeric},
	},
	ast.ExprBinaryBitAnd: {
		{Left: FamilyIntegral, Right: FamilyIntegral, Result: BinaryResultNumeric},
	},
	ast.ExprBinaryBitOr: {
		{Left: FamilyIntegral, Right: FamilyIntegral, Result: BinaryResultNumeric},
	},
	ast.ExprBinaryBitXor: {
		{Left: FamilyIntegral, Right: FamilyIntegral, Result: BinaryResultNumeric},
	},
	ast.ExprBinaryShiftLeft: {
		{Left: FamilyIntegral, Right: FamilyIntegral, Result: BinaryResultLeft},
	},
	ast.ExprBinaryShiftRight: {
		{Left: FamilyIntegral, Right: FamilyIntegral, Result: BinaryResultLeft},
	},
	ast.ExprBinaryLogicalAnd: {
		{Left: FamilyBool, Right: FamilyBool, Result: BinaryResultBool, Flags: BinaryFlagShortCircuit},
	},
	ast.ExprBinaryLogicalOr: {
		{Left: FamilyBool, Right: FamilyBool, Result: BinaryResultBool, Flags: BinaryFlagShortCircuit},
	},
	ast.ExprBinaryEq: {
		{Left: FamilyAny, Right: FamilyAny, Result: BinaryResultBool, Flags: BinaryFlagSameFamily | BinaryFlagCommutative},
	},
	ast.ExprBinaryNotEq: {
		{Left: FamilyAny, Right: FamilyAny, Result: BinaryResultBool, Flags: BinaryFlagSameFamily | BinaryFlagCommutative},
	},
	ast.ExprBinaryLess: {
		{Left: FamilyNumeric, Right: FamilyNumeric, Result: BinaryResultBool},
	},
	ast.ExprBinaryLessEq: {
		{Left: FamilyNumeric, Right: FamilyNumeric, Result: BinaryResultBool},
	},
	ast.ExprBinaryGreater: {
		{Left: FamilyNumeric, Right: FamilyNumeric, Result: BinaryResultBool},
	},
	ast.ExprBinaryGreaterEq: {
		{Left: FamilyNumeric, Right: FamilyNumeric, Result: BinaryResultBool},
	},
	ast.ExprBinaryAssign: {
		{Left: FamilyAny, Right: FamilyAny, Result: BinaryResultLeft, Flags: BinaryFlagAssignment},
	},
	ast.ExprBinaryAddAssign: {
		{Left: FamilyNumeric, Right: FamilyNumeric, Result: BinaryResultLeft, Flags: BinaryFlagAssignment},
	},
	ast.ExprBinarySubAssign: {
		{Left: FamilyNumeric, Right: FamilyNumeric, Result: BinaryResultLeft, Flags: BinaryFlagAssignment},
	},
	ast.ExprBinaryMulAssign: {
		{Left: FamilyNumeric, Right: FamilyNumeric, Result: BinaryResultLeft, Flags: BinaryFlagAssignment},
	},
	ast.ExprBinaryDivAssign: {
		{Left: FamilyNumeric, Right: FamilyNumeric, Result: BinaryResultLeft, Flags: BinaryFlagAssignment},
	},
	ast.ExprBinaryModAssign: {
		{Left: FamilyIntegral, Right: FamilyIntegral, Result: BinaryResultLeft, Flags: BinaryFlagAssignment},
	},
	ast.ExprBinaryBitAndAssign: {
		{Left: FamilyIntegral, Right: FamilyIntegral, Result: BinaryResultLeft, Flags: BinaryFlagAssignment},
	},
	ast.ExprBinaryBitOrAssign: {
		{Left: FamilyIntegral, Right: FamilyIntegral, Result: BinaryResultLeft, Flags: BinaryFlagAssignment},
	},
	ast.ExprBinaryBitXorAssign: {
		{Left: FamilyIntegral, Right: FamilyIntegral, Result: BinaryResultLeft, Flags: BinaryFlagAssignment},
	},
	ast.ExprBinaryShlAssign: {
		{Left: FamilyIntegral, Right: FamilyIntegral, Result: BinaryResultLeft, Flags: BinaryFlagAssignment},
	},
	ast.ExprBinaryShrAssign: {
		{Left: FamilyIntegral, Right: FamilyIntegral, Result: BinaryResultLeft, Flags: BinaryFlagAssignment},
	},
	ast.ExprBinaryNullCoalescing: {
		{Left: FamilyOptional | FamilyResult | FamilyAny, Right: FamilyAny, Result: BinaryResultLeft, Flags: BinaryFlagShortCircuit},
	},
	ast.ExprBinaryRange: {
		{Left: FamilyNumeric | FamilyAny, Right: FamilyNumeric | FamilyAny, Result: BinaryResultRange},
	},
	ast.ExprBinaryRangeInclusive: {
		{Left: FamilyNumeric | FamilyAny, Right: FamilyNumeric | FamilyAny, Result: BinaryResultRange},
	},
	ast.ExprBinaryIs: {
		{Left: FamilyAny, Right: FamilyAny, Result: BinaryResultBool, Flags: BinaryFlagTypeOperand},
	},
	ast.ExprBinaryHeir: {
		{Left: FamilyAny, Right: FamilyAny, Result: BinaryResultBool, Flags: BinaryFlagTypeOperand},
	},
}

var unarySpecTable = map[ast.ExprUnaryOp]UnarySpec{
	ast.ExprUnaryPlus:  {Operand: FamilyNumeric, Result: UnaryResultNumeric},
	ast.ExprUnaryMinus: {Operand: FamilyNumeric, Result: UnaryResultNumeric},
	ast.ExprUnaryNot:   {Operand: FamilyBool, Result: UnaryResultBool},
	ast.ExprUnaryDeref: {Operand: FamilyPointer | FamilyReference, Result: UnaryResultDeref},
	ast.ExprUnaryRef:   {Operand: FamilyAny, Result: UnaryResultReference, Flags: UnaryFlagRequiresAddressable},
	ast.ExprUnaryAwait: {Operand: FamilyAny, Result: UnaryResultAwait},
}

// BinarySpecs returns operand rules for the given operator.
func BinarySpecs(op ast.ExprBinaryOp) []BinarySpec {
	return binarySpecTable[op]
}

// UnarySpecFor returns operand/result hints for unary operators.
func UnarySpecFor(op ast.ExprUnaryOp) (UnarySpec, bool) {
	spec, ok := unarySpecTable[op]
	return spec, ok
}
