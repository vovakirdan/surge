package diagfmt

import (
	"fmt"
	"surge/internal/source"
)

// formatSpan formats a source.Span into a compact string representation.
// If fs is non-nil it resolves start and end positions and returns "startLine:startCol-endLine:endCol"; otherwise it returns "span(start-end)" using the span's raw Start and End offsets.
func formatSpan(span source.Span, fs *source.FileSet) string {
	if fs != nil {
		start, end := fs.Resolve(span)
		return fmt.Sprintf("%d:%d-%d:%d", start.Line, start.Col, end.Line, end.Col)
	}
	return fmt.Sprintf("span(%d-%d)", span.Start, span.End)
}
