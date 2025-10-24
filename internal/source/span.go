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

// ShiftLeft сдвигает span налево на n байт
func (s Span) ShiftLeft(n uint32) Span {
	if n > s.Start {
		return s
	}
	return Span{
		File:  s.File,
		Start: s.Start - n,
		End:   s.End - n,
	}
}

// ShiftRight сдвигает span направо на n байт
func (s Span) ShiftRight(n uint32) Span {
	if n > s.End-s.Start {
		return s
	}
	return Span{
		File:  s.File,
		Start: s.Start + n,
		End:   s.End + n,
	}
}

// ZeroideToStart возвращает span, где start == end == изначальный start
// используется для Insert операций
func (s Span) ZeroideToStart() Span {
	return Span{
		File:  s.File,
		Start: s.Start,
		End:   s.Start,
	}
}

// ZeroideToEnd возвращает span, где start == end == изначальный end
// используется для Insert операций
func (s Span) ZeroideToEnd() Span {
	return Span{
		File:  s.File,
		Start: s.End,
		End:   s.End,
	}
}
