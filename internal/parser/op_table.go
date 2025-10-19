package parser

import (
	"surge/internal/ast"
	"surge/internal/token"
)

// Таблица приоритетов для бинарных операторов
// Чем больше число, тем выше приоритет
const (
	precAssignment     = 1  // = += -= *= /= %=
	precLogicalOr      = 2  // ||
	precLogicalAnd     = 3  // &&
	precEquality       = 4  // == !=
	precComparison     = 5  // < <= > >=
	precBitwiseOr      = 6  // |
	precBitwiseXor     = 7  // ^
	precBitwiseAnd     = 8  // &
	precShift          = 9  // << >>
	precAdditive       = 10 // + -
	precMultiplicative = 11 // * / %
)

// getBinaryOperatorPrec возвращает приоритет и ассоциативность оператора
// Возвращает (приоритет, правоассоциативный)
func (p *Parser) getBinaryOperatorPrec(kind token.Kind) (int, bool) {
	switch kind {
	// Присваивание (правоассоциативно)
	case token.Assign:
		return precAssignment, true

	// Логические операторы
	case token.OrOr:
		return precLogicalOr, false
	case token.AndAnd:
		return precLogicalAnd, false

	// Операторы равенства
	case token.EqEq, token.BangEq:
		return precEquality, false

	// Операторы сравнения
	case token.Lt, token.LtEq, token.Gt, token.GtEq:
		return precComparison, false

	// Битовые операторы
	case token.Pipe:
		return precBitwiseOr, false
	case token.Caret:
		return precBitwiseXor, false
	case token.Amp:
		return precBitwiseAnd, false

	// Сдвиги
	case token.Shl, token.Shr:
		return precShift, false

	// Арифметические операторы
	case token.Plus, token.Minus:
		return precAdditive, false
	case token.Star, token.Slash, token.Percent:
		return precMultiplicative, false

	default:
		return -1, false // не бинарный оператор
	}
}

// tokenKindToBinaryOp преобразует токен в тип бинарного оператора
func (p *Parser) tokenKindToBinaryOp(kind token.Kind) ast.ExprBinaryOp {
	switch kind {
	// Арифметические
	case token.Plus:
		return ast.ExprBinaryAdd
	case token.Minus:
		return ast.ExprBinarySub
	case token.Star:
		return ast.ExprBinaryMul
	case token.Slash:
		return ast.ExprBinaryDiv
	case token.Percent:
		return ast.ExprBinaryMod

	// Битовые
	case token.Amp:
		return ast.ExprBinaryBitAnd
	case token.Pipe:
		return ast.ExprBinaryBitOr
	case token.Caret:
		return ast.ExprBinaryBitXor
	case token.Shl:
		return ast.ExprBinaryShiftLeft
	case token.Shr:
		return ast.ExprBinaryShiftRight

	// Логические
	case token.AndAnd:
		return ast.ExprBinaryLogicalAnd
	case token.OrOr:
		return ast.ExprBinaryLogicalOr

	// Сравнения
	case token.EqEq:
		return ast.ExprBinaryEq
	case token.BangEq:
		return ast.ExprBinaryNotEq
	case token.Lt:
		return ast.ExprBinaryLess
	case token.LtEq:
		return ast.ExprBinaryLessEq
	case token.Gt:
		return ast.ExprBinaryGreater
	case token.GtEq:
		return ast.ExprBinaryGreaterEq

	// Присваивание
	case token.Assign:
		return ast.ExprBinaryAssign

	default:
		// Это не должно случаться, если таблица приоритетов корректна
		return ast.ExprBinaryAdd // fallback
	}
}

// getUnaryOperator возвращает тип унарного оператора для токена
func (p *Parser) getUnaryOperator(kind token.Kind) (ast.ExprUnaryOp, bool) {
	switch kind {
	case token.Plus:
		return ast.ExprUnaryPlus, true
	case token.Minus:
		return ast.ExprUnaryMinus, true
	case token.Bang:
		return ast.ExprUnaryNot, true
	case token.Star:
		return ast.ExprUnaryDeref, true
	case token.Amp:
		return ast.ExprUnaryRef, true
	case token.KwAwait:
		return ast.ExprUnaryAwait, true
	default:
		return ast.ExprUnaryPlus, false // не унарный оператор
	}
}
