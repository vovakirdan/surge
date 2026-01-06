package format

import (
	"bytes"

	"surge/internal/source"
)

// Writer accumulates formatted output and provides helpers for copying source
// fragments and emitting canonical whitespace.
type Writer struct {
	sf          *source.File
	opt         Options
	buf         []byte
	indentLevel int
	atLineStart bool
}

// NewWriter creates a new formatting writer.
func NewWriter(sf *source.File, opt Options) *Writer {
	return &Writer{
		sf:          sf,
		opt:         opt.withDefaults(),
		buf:         make([]byte, 0, len(sf.Content)),
		atLineStart: false,
	}
}

// Bytes returns the accumulated formatted output.
func (w *Writer) Bytes() []byte {
	return w.buf
}

func (w *Writer) writeIndent() {
	if !w.atLineStart {
		return
	}
	if w.opt.UseTabs {
		for range w.indentLevel {
			w.buf = append(w.buf, '\t')
		}
	} else {
		spaceCount := w.indentLevel * w.opt.IndentWidth
		for range spaceCount {
			w.buf = append(w.buf, ' ')
		}
	}
	w.atLineStart = false
}

// WriteString writes a string to the output, handling indentation.
func (w *Writer) WriteString(s string) {
	if s == "" {
		return
	}
	w.writeIndent()
	w.buf = append(w.buf, s...)
	w.updateLineState(s[len(s)-1])
}

// WriteByte writes a single byte to the output.
func (w *Writer) WriteByte(b byte) error {
	w.writeIndent()
	w.buf = append(w.buf, b)
	w.updateLineState(b)
	return nil
}

func (w *Writer) updateLineState(last byte) {
	if last == '\n' {
		w.atLineStart = true
	} else {
		w.atLineStart = false
	}
}

// Space writes a single space if the output doesn't already end with whitespace.
func (w *Writer) Space() {
	if len(w.buf) == 0 {
		return
	}
	last := w.buf[len(w.buf)-1]
	if last == ' ' || last == '\n' || last == '\t' {
		return
	}
	w.buf = append(w.buf, ' ')
}

// MaybeSpace writes a space if the condition is true.
func (w *Writer) MaybeSpace(cond bool) {
	if cond {
		w.Space()
	}
}

// Newline writes a newline if the output doesn't already end with one.
func (w *Writer) Newline() {
	if len(w.buf) == 0 || w.buf[len(w.buf)-1] != '\n' {
		w.buf = append(w.buf, '\n')
	}
	w.atLineStart = true
}

// IndentPush increases the indentation level.
func (w *Writer) IndentPush() {
	w.indentLevel++
}

// IndentPop decreases the indentation level.
func (w *Writer) IndentPop() {
	if w.indentLevel > 0 {
		w.indentLevel--
	}
}

// CopySpan copies a span from the source file to the output.
func (w *Writer) CopySpan(sp source.Span) {
	if !spanValid(sp) || w.sf == nil || sp.File != w.sf.ID {
		return
	}
	start, end := int(sp.Start), int(sp.End)
	w.CopyRange(start, end)
}

// CopyRange copies a range of bytes from the source file to the output.
func (w *Writer) CopyRange(start, end int) {
	if w.sf == nil {
		return
	}
	if start < 0 {
		start = 0
	}
	if end > len(w.sf.Content) {
		end = len(w.sf.Content)
	}
	if start >= end {
		return
	}
	chunk := w.sf.Content[start:end]
	w.buf = append(w.buf, chunk...)
	w.updateLineState(chunk[len(chunk)-1])
}

// TrimmedCopySpan copies a span from the source file to the output, trimming leading/trailing whitespace.
func (w *Writer) TrimmedCopySpan(sp source.Span) {
	if !spanValid(sp) || w.sf == nil || sp.File != w.sf.ID {
		return
	}
	start, end := int(sp.Start), int(sp.End)
	if start < 0 {
		start = 0
	}
	if end > len(w.sf.Content) {
		end = len(w.sf.Content)
	}
	if start >= end {
		return
	}
	trimmed := bytes.TrimSpace(w.sf.Content[start:end])
	if len(trimmed) == 0 {
		return
	}
	w.writeIndent()
	w.buf = append(w.buf, trimmed...)
	w.updateLineState(trimmed[len(trimmed)-1])
}
