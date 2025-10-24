package parser

import (
	"surge/internal/ast"
	"surge/internal/token"
)

// Таблица приоритетов для бинарных операторов
// Чем больше число, тем выше приоритет
const (
	precAssignment     = 1 // = += -= *= /= %= &= |= ^= <<= >>=
	precNullCoalescing = 2 // ??
	// precTernary зарезервирован под тернарный оператор `?:`.
	// Он разбирается отдельной веткой, поэтому в таблице пока не используется.
	precTernary        = 3  // ? : (right-associative)
	precLogicalOr      = 4  // ||
	precLogicalAnd     = 5  // &&
	precComparison     = 6  // < <= > >= == != is
	precRange          = 7  // .. ..=
	precBitwiseOr      = 8  // |
	precBitwiseXor     = 9  // ^
	precBitwiseAnd     = 10 // &
	precShift          = 11 // << >>
	precAdditive       = 12 // + -
	precMultiplicative = 13 // * / %
)

// getBinaryOperatorPrec возвращает приоритет и ассоциативность оператора
// Возвращает (приоритет, правоассоциативный)
func (p *Parser) getBinaryOperatorPrec(kind token.Kind) (int, bool) {
	switch kind {
	// Присваивание (правоассоциативно)
	case token.Assign, token.PlusAssign, token.MinusAssign, token.StarAssign,
		token.SlashAssign, token.PercentAssign, token.AmpAssign, token.PipeAssign,
		token.CaretAssign, token.ShlAssign, token.ShrAssign:
		return precAssignment, true

	// Null coalescing
	case token.QuestionQuestion:
		return precNullCoalescing, false

	// Логические операторы
	case token.OrOr:
		return precLogicalOr, false
	case token.AndAnd:
		return precLogicalAnd, false

	// Операторы сравнения (включая is)
	case token.EqEq, token.BangEq, token.Lt, token.LtEq, token.Gt, token.GtEq, token.KwIs:
		return precComparison, false

	// Range операторы
	case token.DotDot, token.DotDotEq:
		return precRange, false

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
	case token.PlusAssign:
		return ast.ExprBinaryAddAssign
	case token.MinusAssign:
		return ast.ExprBinarySubAssign
	case token.StarAssign:
		return ast.ExprBinaryMulAssign
	case token.SlashAssign:
		return ast.ExprBinaryDivAssign
	case token.PercentAssign:
		return ast.ExprBinaryModAssign
	case token.AmpAssign:
		return ast.ExprBinaryBitAndAssign
	case token.PipeAssign:
		return ast.ExprBinaryBitOrAssign
	case token.CaretAssign:
		return ast.ExprBinaryBitXorAssign
	case token.ShlAssign:
		return ast.ExprBinaryShlAssign
	case token.ShrAssign:
		return ast.ExprBinaryShrAssign

	// Специальные операторы
	case token.QuestionQuestion:
		return ast.ExprBinaryNullCoalescing
	case token.DotDot:
		return ast.ExprBinaryRange
	case token.DotDotEq:
		return ast.ExprBinaryRangeInclusive
	case token.KwIs:
		return ast.ExprBinaryIs

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
