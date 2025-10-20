package source

import (
	"fmt"
)

type Span struct {
	File  FileID
	Start uint32 // в байтах включительно
	End   uint32 // в байтах не включительно
}

func (s Span) Empty() bool {
	return s.Start == s.End
}

func (s Span) Len() uint32 {
	return s.End - s.Start
}

func (s Span) String() string {
	return fmt.Sprintf("%d:%d-%d", s.File, s.Start, s.End)
}

func (s Span) Cover(other Span) Span {
	if s.File != other.File {
		return s
	}
	if other.Start < s.Start {
		s.Start = other.Start
	}
	if other.End > s.End {
		s.End = other.End
	}
	return s
}

func (s Span) ShiftLeft(n uint32) Span {
	return Span{
		File:  s.File,
		Start: s.Start - n,
		End:   s.End - n,
	}
}

func (s Span) ShiftRight(n uint32) Span {
	return Span{
		File:  s.File,
		Start: s.Start + n,
		End:   s.End + n,
	}
}
