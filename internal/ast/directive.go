package ast

import "surge/internal/source"

// DirectiveBlock contains directives collected from doc comments.
type DirectiveBlock struct {
	Namespace source.StringID
	Lines     []DirectiveLine
	Span      source.Span
	Owner     ItemID // NoItemID for file-level directives.
}

// DirectiveLine represents a single directive expression line as written in the source.
type DirectiveLine struct {
	Text source.StringID
	Span source.Span
}
