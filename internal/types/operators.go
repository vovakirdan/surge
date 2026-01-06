package types

import "surge/internal/ast"

// FamilyMask describes broad categories of types an operator accepts.
type FamilyMask uint32

const (
	// FamilyNone indicates no type family.
	FamilyNone FamilyMask = 0
	// FamilyAny indicates any type family.
	FamilyAny FamilyMask = 1 << iota
	// FamilyBool indicates boolean type family.
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
	// FamilyIntegral represents integral types (signed and unsigned integers).
	FamilyIntegral = FamilySignedInt | FamilyUnsignedInt
	// FamilyNumeric represents numeric types (integral and float).
	FamilyNumeric = FamilyIntegral | FamilyFloat
)

// BinaryResult describes how to derive the result type for an operator.
type BinaryResult uint8

const (
	// BinaryResultUnknown indicates the result type is unknown.
	BinaryResultUnknown BinaryResult = iota
	// BinaryResultLeft indicates the result type is the left operand type.
	BinaryResultLeft
	// BinaryResultRight indicates the result type is the right operand type.
	BinaryResultRight
	BinaryResultBool
	BinaryResultRange
)

// BinaryFlags annotate special handling for binary operators.
type BinaryFlags uint16

const (
	// BinaryFlagNone indicates no binary flags.
	BinaryFlagNone BinaryFlags = 0
	// BinaryFlagShortCircuit indicates a short-circuit binary operator.
	BinaryFlagShortCircuit BinaryFlags = 1 << iota
)

// BinarySpec lists operand families and expected result for an operation.
type BinarySpec struct {
	Left   FamilyMask
	Right  FamilyMask
	Result BinaryResult
	Flags  BinaryFlags
}

var binarySpecTable = map[ast.ExprBinaryOp][]BinarySpec{
	ast.ExprBinaryLogicalAnd: {
		{Left: FamilyBool, Right: FamilyBool, Result: BinaryResultBool, Flags: BinaryFlagShortCircuit},
	},
	ast.ExprBinaryLogicalOr: {
		{Left: FamilyBool, Right: FamilyBool, Result: BinaryResultBool, Flags: BinaryFlagShortCircuit},
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
		{Left: FamilyAny, Right: FamilyAny, Result: BinaryResultBool},
	},
	ast.ExprBinaryHeir: {
		{Left: FamilyAny, Right: FamilyAny, Result: BinaryResultBool},
	},
}

// BinarySpecs returns operand rules for the given operator.
func BinarySpecs(op ast.ExprBinaryOp) []BinarySpec {
	return binarySpecTable[op]
}
