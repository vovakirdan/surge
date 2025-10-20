package diagfmt

import (
	"fmt"
	"surge/internal/source"
)

func formatSpan(span source.Span, fs *source.FileSet) string {
	if fs != nil {
		start, end := fs.Resolve(span)
		return fmt.Sprintf("%d:%d-%d:%d", start.Line, start.Col, end.Line, end.Col)
	}
	return fmt.Sprintf("span(%d-%d)", span.Start, span.End)
}
