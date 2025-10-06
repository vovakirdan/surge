package token

import "surge/internal/source"

//go:generate stringer -type=TriviaKind -trimprefix=Trivia
type Directive struct {
	Module  string
	Name    string
	Payload string
}

type TriviaKind uint8

const (
	TriviaSpace TriviaKind = iota
	TriviaNewline
	TriviaLineComment
	TriviaBlockComment
	TriviaDocLine
	TriviaDocBlock
	TriviaDirective
)

type Trivia struct {
	Kind      TriviaKind
	Span      source.Span
	Text      string
	Directive *Directive // только если Kind == TriviaDirective
}
