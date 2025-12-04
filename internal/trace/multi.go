package trace

// MultiTracer fans out trace events to multiple tracers.
type MultiTracer struct {
	tracers []Tracer
	level   Level
}

// NewMultiTracer creates a new MultiTracer that emits to all provided tracers.
func NewMultiTracer(level Level, tracers ...Tracer) *MultiTracer {
	return &MultiTracer{
		tracers: tracers,
		level:   level,
	}
}

// Emit sends the event to all underlying tracers.
func (t *MultiTracer) Emit(ev Event) {
	for _, tr := range t.tracers {
		tr.Emit(ev)
	}
}

// Flush flushes all underlying tracers.
func (t *MultiTracer) Flush() error {
	var firstErr error
	for _, tr := range t.tracers {
		if err := tr.Flush(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Close closes all underlying tracers.
func (t *MultiTracer) Close() error {
	var firstErr error
	for _, tr := range t.tracers {
		if err := tr.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Level returns the configured level.
func (t *MultiTracer) Level() Level {
	return t.level
}

// Enabled returns true if tracing is active.
func (t *MultiTracer) Enabled() bool {
	return t.level > LevelOff
}
