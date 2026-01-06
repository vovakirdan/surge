package ast

import (
	"surge/internal/dialect"
	"surge/internal/source"
)

// File represents a source file in the AST.
type File struct {
	Span source.Span
	// DialectEvidence records "alien dialect" signals collected during parsing.
	// It is always safe to ignore; it never affects parsing or semantic behavior.
	DialectEvidence *dialect.Evidence
	Items           []ItemID
	Pragma          Pragma
	Directives      []DirectiveBlock
}

// Files manages allocation of File nodes.
type Files struct {
	Arena *Arena[File]
}

// NewFiles creates a new Files arena with the given capacity hint.
func NewFiles(capHint uint) *Files {
	return &Files{
		Arena: NewArena[File](capHint),
	}
}

// New creates a new file in the arena.
func (f *Files) New(sp source.Span) FileID {
	return FileID(f.Arena.Allocate(File{
		Span:       sp,
		Items:      make([]ItemID, 0),
		Pragma:     Pragma{},
		Directives: make([]DirectiveBlock, 0),
	}))
}

// Get returns the file with the given ID.
func (f *Files) Get(id FileID) *File {
	return f.Arena.Get(uint32(id))
}
