package ast

import "surge/internal/source"

// Attr описывает пользовательский атрибут вида `@name(args...)`.
type Attr struct {
	Name source.StringID
	Args []ExprID
	Span source.Span
}
