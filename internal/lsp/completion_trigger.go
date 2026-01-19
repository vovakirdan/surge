package lsp

import (
	"sort"

	"surge/internal/source"
	"surge/internal/token"
)

func completionTrigger(tokens []token.Token, offset uint32, idx int) (kind token.Kind, targetOffset uint32) {
	if len(tokens) == 0 || idx < 0 {
		return token.Invalid, 0
	}
	current := tokens[idx]
	if (current.Kind == token.Dot || current.Kind == token.ColonColon) && offset >= current.Span.End {
		return current.Kind, beforeTokenOffset(current)
	}
	if idx > 0 {
		prev := tokens[idx-1]
		if (prev.Kind == token.Dot || prev.Kind == token.ColonColon) && current.Kind == token.Ident {
			return prev.Kind, beforeTokenOffset(prev)
		}
	}
	return token.Invalid, 0
}

func beforeTokenOffset(tok token.Token) uint32 {
	if tok.Span.Start == 0 {
		return 0
	}
	return tok.Span.Start - 1
}

func isTypePosition(tokens []token.Token, offset uint32) bool {
	prevIdx := tokenIndexBeforeOffset(tokens, offset)
	if prevIdx < 0 {
		return false
	}
	switch tokens[prevIdx].Kind {
	case token.Colon, token.Arrow, token.KwAs, token.KwTo, token.KwIs, token.KwHeir:
		return true
	default:
		return false
	}
}

func tokenIndexAtOffset(tokens []token.Token, offset uint32) int {
	if len(tokens) == 0 {
		return -1
	}
	idx := sort.Search(len(tokens), func(i int) bool { return tokens[i].Span.End > offset })
	if idx < len(tokens) {
		tok := tokens[idx]
		if tok.Span.Start <= offset && offset < tok.Span.End {
			return idx
		}
	}
	if idx > 0 {
		prev := tokens[idx-1]
		if prev.Span.Start <= offset && offset == prev.Span.End {
			return idx - 1
		}
	}
	if idx < len(tokens) && tokens[idx].Span.Start == offset {
		return idx
	}
	return -1
}

func tokenIndexBeforeOffset(tokens []token.Token, offset uint32) int {
	if len(tokens) == 0 {
		return -1
	}
	idx := sort.Search(len(tokens), func(i int) bool { return tokens[i].Span.End > offset })
	if idx-1 >= 0 {
		return idx - 1
	}
	return -1
}

func sliceContent(file *source.File, start, end uint32) string {
	if file == nil || start >= end {
		return ""
	}
	contentLen := safeUint32(len(file.Content))
	if end > contentLen {
		end = contentLen
	}
	if start >= end {
		return ""
	}
	return string(file.Content[start:end])
}
