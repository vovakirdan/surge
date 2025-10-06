package token_test

import (
	"testing"

	"surge/internal/source"
	"surge/internal/token"
)

func TestDirectiveTriviaShape(t *testing.T) {
	dir := &token.Directive{
		Module:  "surge.token",
		Name:    "keywords-pass",
		Payload: "cover int8/uint8",
	}
	tv := token.Trivia{
		Kind:      token.TriviaDirective,
		Span:      source.Span{Start: 0, End: 10},
		Text:      "/// directive...",
		Directive: dir,
	}
	tok := token.Token{
		Kind:    token.KwFn,
		Span:    source.Span{Start: 42, End: 44},
		Text:    "fn",
		Leading: []token.Trivia{tv},
	}
	if len(tok.Leading) != 1 || tok.Leading[0].Kind != token.TriviaDirective || tok.Leading[0].Directive == nil {
		t.Fatalf("directive trivia must be present and structured")
	}
}
