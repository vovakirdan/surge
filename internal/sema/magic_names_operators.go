package sema

import "surge/internal/ast"

// Which magic method an operator asks for.
//
// Only the spelling lives here: `+` asks for __add, `-` in prefix position
// asks for __neg. Whether a type ANSWERS that question is decided elsewhere;
// this file would be a lookup table if Go spelled one as nicely as a switch.

func magicNameForBinaryOp(op ast.ExprBinaryOp) string {
	switch op {
	case ast.ExprBinaryAdd:
		return "__add"
	case ast.ExprBinarySub:
		return "__sub"
	case ast.ExprBinaryMul:
		return "__mul"
	case ast.ExprBinaryDiv:
		return "__div"
	case ast.ExprBinaryMod:
		return "__mod"
	case ast.ExprBinaryBitAnd:
		return "__bit_and"
	case ast.ExprBinaryBitOr:
		return "__bit_or"
	case ast.ExprBinaryBitXor:
		return "__bit_xor"
	case ast.ExprBinaryShiftLeft:
		return "__shl"
	case ast.ExprBinaryShiftRight:
		return "__shr"
	case ast.ExprBinaryEq:
		return "__eq"
	case ast.ExprBinaryNotEq:
		return "__ne"
	case ast.ExprBinaryLess:
		return "__lt"
	case ast.ExprBinaryLessEq:
		return "__le"
	case ast.ExprBinaryGreater:
		return "__gt"
	case ast.ExprBinaryGreaterEq:
		return "__ge"
	default:
		return ""
	}
}

func magicNameForUnaryOp(op ast.ExprUnaryOp) string {
	switch op {
	case ast.ExprUnaryPlus:
		return "__pos"
	case ast.ExprUnaryMinus:
		return "__neg"
	case ast.ExprUnaryNot:
		return "__not"
	default:
		return ""
	}
}
