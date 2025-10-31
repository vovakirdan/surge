package ast

import (
	"surge/internal/source"
)

type File struct {
	Span       source.Span
	Items      []ItemID
	Pragma     Pragma
	Directives []DirectiveBlock
}

type Files struct {
	Arena *Arena[File]
}

func NewFiles(capHint uint) *Files {
	return &Files{
		Arena: NewArena[File](capHint),
	}
}

func (f *Files) New(sp source.Span) FileID {
	return FileID(f.Arena.Allocate(File{
		Span:       sp,
		Items:      make([]ItemID, 0),
		Pragma:     Pragma{},
		Directives: make([]DirectiveBlock, 0),
	}))
}

func (f *Files) Get(id FileID) *File {
	return f.Arena.Get(uint32(id))
}
