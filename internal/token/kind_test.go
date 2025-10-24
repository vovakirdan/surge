package token_test

import (
	"testing"

	"surge/internal/source"
	"surge/internal/token"
)

func tok(k token.Kind) token.Token {
	return token.Token{Kind: k, Span: source.Span{Start: 0, End: 0}}
}

func TestIsLiteral(t *testing.T) {
	lits := []token.Kind{
		token.NothingLit, token.IntLit, token.UintLit,
		token.FloatLit, token.BoolLit, token.StringLit,
	}
	for _, k := range lits {
		if !tok(k).IsLiteral() {
			t.Fatalf("%v should be literal", k)
		}
	}
	non := []token.Kind{token.Ident, token.KwLet, token.Plus, token.LParen}
	for _, k := range non {
		if tok(k).IsLiteral() {
			t.Fatalf("%v must NOT be literal", k)
		}
	}
}

func TestIsPunctOrOp(t *testing.T) {
	ops := []token.Kind{
		token.Plus, token.Minus, token.Star, token.Slash, token.Percent,
		token.Assign, token.PlusAssign, token.MinusAssign, token.StarAssign,
		token.SlashAssign, token.PercentAssign, token.AmpAssign, token.PipeAssign,
		token.CaretAssign, token.ShlAssign, token.ShrAssign,
		token.EqEq, token.Bang, token.BangEq,
		token.Lt, token.LtEq, token.Gt, token.GtEq,
		token.Shl, token.Shr, token.Amp, token.Pipe, token.Caret,
		token.AndAnd, token.OrOr,
		token.Question, token.QuestionQuestion, token.Colon, token.ColonColon,
		token.Semicolon, token.Comma,
		token.Dot, token.DotDot, token.DotDotEq, token.Arrow, token.FatArrow,
		token.LParen, token.RParen, token.LBrace, token.RBrace, token.LBracket, token.RBracket,
		token.At, token.Underscore, token.ColonAssign,
	}
	for _, k := range ops {
		if !tok(k).IsPunctOrOp() {
			t.Fatalf("%v should be punct/op", k)
		}
	}
	non := []token.Kind{token.Ident, token.KwIf, token.IntLit}
	for _, k := range non {
		if tok(k).IsPunctOrOp() {
			t.Fatalf("%v must NOT be punct/op", k)
		}
	}
}

func TestIsIdent(t *testing.T) {
	if !tok(token.Ident).IsIdent() {
		t.Fatalf("Ident should be ident")
	}
	if tok(token.KwFn).IsIdent() {
		t.Fatalf("KwFn must not be ident")
	}
}

func TestIsKeyword(t *testing.T) {
	keywords := []token.Kind{
		token.KwFn, token.KwLet, token.KwMut, token.KwOwn, token.KwIf, token.KwElse, token.KwWhile,
		token.KwFor, token.KwIn, token.KwBreak, token.KwContinue, token.KwReturn, token.KwImport,
		token.KwAs, token.KwType, token.KwNewtype, token.KwAlias, token.KwLiteral, token.KwTag,
		token.KwExtern, token.KwPub, token.KwAsync, token.KwAwait, token.KwCompare, token.KwFinally,
		token.KwChannel, token.KwSpawn, token.KwTrue, token.KwFalse, token.KwSignal, token.KwParallel,
		token.KwMacro, token.KwPragma, token.KwTo, token.KwHeir, token.KwIs,
	}
	for _, k := range keywords {
		if !tok(k).IsKeyword() {
			t.Fatalf("%v should be keyword", k)
		}
	}
}
