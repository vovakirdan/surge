package token

import "surge/internal/source"

// Directive represents a source-level directive comment (e.g. //@test).
//
//go:generate stringer -type=TriviaKind -trimprefix=Trivia
type Directive struct {
	Module  string
	Name    string
	Payload string
}

// TriviaKind classifies types of non-code elements.
type TriviaKind uint8

const (
	// TriviaSpace represents horizontal whitespace.
	TriviaSpace TriviaKind = iota
	// TriviaNewline represents a newline character.
	TriviaNewline
	// TriviaLineComment represents a line comment.
	TriviaLineComment
	// TriviaBlockComment represents a block comment.
	TriviaBlockComment
	// TriviaDocLine represents a doc line.
	TriviaDocLine
	// TriviaDocBlock represents a doc block.
	TriviaDocBlock
	// TriviaDirective represents a directive.
	TriviaDirective
)

// Trivia represents a non-code source element like comments or whitespace.
type Trivia struct {
	Kind      TriviaKind
	Span      source.Span
	Text      string
	Directive *Directive // только если Kind == TriviaDirective
}
