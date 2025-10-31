package token

import (
	"surge/internal/source"
)

type Token struct {
	Kind    Kind
	Span    source.Span
	Text    string
	Leading []Trivia
}

func (t Token) IsLiteral() bool {
	switch t.Kind {
	case NothingLit, IntLit, UintLit, FloatLit, BoolLit, StringLit:
		return true
	default:
		return false
	}
}

func (t Token) IsPunctOrOp() bool {
	switch t.Kind {
	case Plus, Minus, Star, Slash, Percent, Assign, PlusAssign, MinusAssign, StarAssign,
		SlashAssign, PercentAssign, AmpAssign, PipeAssign, CaretAssign, ShlAssign, ShrAssign,
		EqEq, Bang, BangEq, Lt, LtEq, Gt, GtEq, Shl, Shr, Amp, Pipe, Caret, AndAnd, OrOr,
		Question, QuestionQuestion, Colon, ColonColon, Semicolon, Comma, Dot, DotDot, Arrow,
		FatArrow, LParen, RParen, LBrace, RBrace, LBracket, RBracket, At, Underscore,
		DotDotEq, ColonAssign:
		return true
	default:
		return false
	}
}

func (t Token) IsKeyword() bool {
	switch t.Kind {
	case KwFn, KwLet, KwMut, KwOwn, KwIf, KwElse, KwWhile, KwFor, KwIn, KwBreak, KwContinue, KwReturn,
		KwImport, KwAs, KwType, KwTag, KwExtern, KwPub, KwAsync, KwAwait,
		KwCompare, KwFinally, KwChannel, KwSpawn, KwTrue, KwFalse, KwSignal, KwParallel, KwMacro,
		KwPragma, KwTo, KwHeir, KwIs:
		return true
	default:
		return false
	}
}

func (t Token) IsIdent() bool { return t.Kind == Ident }
