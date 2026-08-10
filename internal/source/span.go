package source

import (
	"fmt"
)

// NoSpanText is what a location reads as when the span names nothing: no file
// set to resolve it against, or a span that was never filled in.
const NoSpanText = "<no-span>"

// Span represents a contiguous range of bytes within a source file.
type Span struct {
	File  FileID
	Start uint32 // в байтах включительно
	End   uint32 // в байтах не включительно
}

// Empty reports whether the span has zero length.
func (s Span) Empty() bool {
	return s.Start == s.End
}

// Len returns the length of the span in bytes.
func (s Span) Len() uint32 {
	return s.End - s.Start
}

func (s Span) String() string {
	return fmt.Sprintf("%d:%d-%d", s.File, s.Start, s.End)
}

// Cover returns a new span that covers both spans.
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

// ExtendRight расширяет span до начала другого span не включительно.
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

// IsLeftThan reports whether this span starts before another span.
func (s Span) IsLeftThan(other Span) bool {
	return s.File == other.File && s.Start < other.Start
}

// IsRightThan reports whether this span ends after another span.
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

// FormatSpan renders a span the way a runtime panic names its location —
// "path:line:col" at the span's start, or NoSpanText when there is nothing to
// name. Both backends report a panic's location through this one function, so
// the line a program prints does not depend on which of them ran it: the VM
// resolves the span as it panics, and the native backend resolves the same span
// while it emits the panic call.
func FormatSpan(span Span, files *FileSet) string {
	if files == nil || (span.Start == 0 && span.End == 0) {
		return NoSpanText
	}
	// Get and Resolve both index the file table directly, so a span carrying a
	// FileID this set never loaded has to be turned away before it reaches them.
	if !files.HasFile(span.File) {
		return NoSpanText
	}
	file := files.Get(span.File)
	if file == nil {
		return NoSpanText
	}
	start, _ := files.Resolve(span)
	return fmt.Sprintf("%s:%d:%d", file.Path, start.Line, start.Col)
}
