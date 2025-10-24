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

// ExtendUntil расширяет span до начала другого span не включительно
func (s Span) ExtendRight(other Span) Span {
	if s.File != other.File {
		return s
	}
	// Если текущий span заканчивается раньше начала другого span,
	// расширяем его до начала другого span
	if s.End < other.Start {
		return Span{
			File:  s.File,
			Start: s.Start,
			End:   other.Start,
		}
	}
	return s
}

// ExtendLeft расширяет span до конца другого span не включительно
func (s Span) ExtendLeft(other Span) Span {
	if s.File != other.File {
		return s
	}
	if s.Start > other.End {
		return Span{
			File:  s.File,
			Start: other.End,
			End:   s.End,
		}
	}
	return s
}

func (s Span) IsLeftThan(other Span) bool {
	return s.File == other.File && s.Start < other.Start
}

func (s Span) IsRightThan(other Span) bool {
	return s.File == other.File && s.End > other.End
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
