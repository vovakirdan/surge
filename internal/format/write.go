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

func NewWriter(sf *source.File, opt Options) *Writer {
	return &Writer{
		sf:          sf,
		opt:         opt.withDefaults(),
		buf:         make([]byte, 0, len(sf.Content)),
		atLineStart: false,
	}
}

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

func (w *Writer) WriteString(s string) {
	if s == "" {
		return
	}
	w.writeIndent()
	w.buf = append(w.buf, s...)
	w.updateLineState(s[len(s)-1])
}

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

func (w *Writer) MaybeSpace(cond bool) {
	if cond {
		w.Space()
	}
}

func (w *Writer) Newline() {
	if len(w.buf) == 0 || w.buf[len(w.buf)-1] != '\n' {
		w.buf = append(w.buf, '\n')
	}
	w.atLineStart = true
}

func (w *Writer) IndentPush() {
	w.indentLevel++
}

func (w *Writer) IndentPop() {
	if w.indentLevel > 0 {
		w.indentLevel--
	}
}

func (w *Writer) CopySpan(sp source.Span) {
	if !spanValid(sp) || w.sf == nil || sp.File != w.sf.ID {
		return
	}
	start, end := int(sp.Start), int(sp.End)
	w.CopyRange(start, end)
}

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
