package diagfmt

import (
	"fmt"
	"surge/internal/source"
)

// formatSpan formats a source.Span into a string.
// If fs is non-nil, it resolves the span to start and end positions and returns "startLine:startCol-endLine:endCol".
// If fs is nil, it returns "span(start-end)".
func formatSpan(span source.Span, fs *source.FileSet) string {
	if fs != nil {
		start, end := fs.Resolve(span)
		return fmt.Sprintf("%d:%d-%d:%d", start.Line, start.Col, end.Line, end.Col)
	}
	return fmt.Sprintf("span(%d-%d)", span.Start, span.End)
}