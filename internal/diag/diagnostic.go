package diag

import (
	"surge/internal/source"
)

type Note struct {
	Span source.Span
	Msg string
}

type FixEdit struct {
	Span source.Span
	NewText string
}

type Fix struct {
	Title string
	Edits []FixEdit
}

type Diagnostic struct {
	Severity Severity
	Code Code
	Message string
	Primary source.Span
	Notes []Note
	Fixes []Fix
}
