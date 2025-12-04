package trace

// nopTracer is a no-op implementation for zero overhead when tracing is disabled.
type nopTracer struct{}

// Emit does nothing.
func (nopTracer) Emit(Event) {}

// Flush does nothing.
func (nopTracer) Flush() error { return nil }

// Close does nothing.
func (nopTracer) Close() error { return nil }

// Level returns LevelOff.
func (nopTracer) Level() Level { return LevelOff }

// Enabled always returns false.
func (nopTracer) Enabled() bool { return false }

// Nop is the package-level singleton nop tracer.
var Nop Tracer = nopTracer{}
