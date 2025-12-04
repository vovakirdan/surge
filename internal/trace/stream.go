package trace

import (
	"io"
	"sync"
)

// StreamTracer writes events immediately to an io.Writer.
type StreamTracer struct {
	mu     sync.Mutex
	w      io.Writer
	level  Level
	format Format
}

// NewStreamTracer creates a new StreamTracer.
func NewStreamTracer(w io.Writer, level Level, format Format) *StreamTracer {
	return &StreamTracer{
		w:      w,
		level:  level,
		format: format,
	}
}

// Emit writes an event to the output.
func (t *StreamTracer) Emit(ev Event) {
	if !t.level.ShouldEmit(ev.Scope) && ev.Kind != KindHeartbeat {
		return
	}

	ev.Seq = NextSeq()

	data := FormatEvent(ev, t.format)

	t.mu.Lock()
	defer t.mu.Unlock()

	// Best-effort write - don't fail the compilation on trace errors
	_, _ = t.w.Write(data)
}

// Flush ensures all buffered data is written.
// For StreamTracer this is a no-op since we write immediately.
func (t *StreamTracer) Flush() error {
	// If writer has Flush method, call it
	if flusher, ok := t.w.(interface{ Flush() error }); ok {
		return flusher.Flush()
	}
	return nil
}

// Close flushes and closes the writer if it implements io.Closer.
func (t *StreamTracer) Close() error {
	t.Flush()
	if closer, ok := t.w.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// Level returns the current tracing level.
func (t *StreamTracer) Level() Level {
	return t.level
}

// Enabled returns true if tracing is active.
func (t *StreamTracer) Enabled() bool {
	return t.level > LevelOff
}
