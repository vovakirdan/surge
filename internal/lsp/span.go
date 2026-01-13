package lsp

import (
	"sort"
	"unicode/utf8"

	"fortio.org/safecast"

	"surge/internal/source"
)

const maxUint32 = ^uint32(0)

func safeUint32(n int) uint32 {
	if n < 0 {
		return 0
	}
	v, err := safecast.Conv[uint32](n)
	if err != nil {
		return maxUint32
	}
	return v
}

func offsetForPositionInFile(file *source.File, pos position) uint32 {
	if file == nil || pos.Line < 0 || pos.Character < 0 {
		return 0
	}
	content := file.Content
	if len(content) == 0 {
		return 0
	}
	lineCount := len(file.LineIdx) + 1
	contentLen := safeUint32(len(content))
	if pos.Line >= lineCount {
		return contentLen
	}
	var lineStart uint32
	if pos.Line == 0 {
		lineStart = 0
	} else {
		lineStart = file.LineIdx[pos.Line-1] + 1
	}
	lineEnd := contentLen
	if pos.Line < len(file.LineIdx) {
		lineEnd = file.LineIdx[pos.Line]
	}
	if lineStart > lineEnd {
		return lineEnd
	}
	units := 0
	off := lineStart
	for off < lineEnd {
		r, size := utf8.DecodeRune(content[off:lineEnd])
		if r == utf8.RuneError && size == 1 {
			size = 1
		}
		need := 1
		if r > 0xFFFF {
			need = 2
		}
		if units+need > pos.Character {
			break
		}
		units += need
		off += safeUint32(size)
		if units == pos.Character {
			break
		}
	}
	return off
}

func positionForOffsetInFile(file *source.File, offset uint32) position {
	if file == nil {
		return position{}
	}
	contentLen := safeUint32(len(file.Content))
	if offset > contentLen {
		offset = contentLen
	}
	lineIdx := file.LineIdx
	idx := sort.Search(len(lineIdx), func(i int) bool { return lineIdx[i] >= offset })
	line := idx
	var lineStart uint32
	if idx == 0 {
		lineStart = 0
	} else {
		lineStart = lineIdx[idx-1] + 1
	}
	if lineStart > offset {
		lineStart = offset
	}
	units := 0
	for off := lineStart; off < offset; {
		r, size := utf8.DecodeRune(file.Content[off:offset])
		if r == utf8.RuneError && size == 1 {
			size = 1
		}
		if off+safeUint32(size) > offset {
			break
		}
		if r > 0xFFFF {
			units += 2
		} else {
			units++
		}
		off += safeUint32(size)
	}
	return position{Line: line, Character: units}
}

func rangeForSpan(file *source.File, span source.Span) lspRange {
	if file == nil {
		return lspRange{}
	}
	return lspRange{
		Start: positionForOffsetInFile(file, span.Start),
		End:   positionForOffsetInFile(file, span.End),
	}
}
