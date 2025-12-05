package trace

import (
	"io"
	"sync"
)

// RingTracer keeps the last N events in memory (circular buffer).
type RingTracer struct {
	mu       sync.RWMutex
	events   []Event
	capacity int
	head     int  // next write position
	full     bool // has wrapped around
	level    Level
}

// NewRingTracer creates a new RingTracer with specified capacity.
func NewRingTracer(capacity int, level Level) *RingTracer {
	if capacity <= 0 {
		capacity = 4096
	}

	return &RingTracer{
		events:   make([]Event, capacity),
		capacity: capacity,
		level:    level,
	}
}

// Emit adds an event to the ring buffer.
func (t *RingTracer) Emit(ev *Event) {
	if !t.level.ShouldEmit(ev.Scope) && ev.Kind != KindHeartbeat {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	stored := *ev
	stored.Seq = NextSeq()
	t.events[t.head] = stored
	t.head = (t.head + 1) % t.capacity

	if t.head == 0 {
		t.full = true
	}
}

// Snapshot returns a copy of all stored events in chronological order.
func (t *RingTracer) Snapshot() []Event {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if !t.full {
		// Not wrapped yet - return [0:head]
		result := make([]Event, t.head)
		copy(result, t.events[:t.head])
		return result
	}

	// Wrapped - return [head:capacity] + [0:head]
	result := make([]Event, t.capacity)
	copy(result, t.events[t.head:])
	copy(result[t.capacity-t.head:], t.events[:t.head])
	return result
}

// Dump writes all events to the provided writer in the specified format.
func (t *RingTracer) Dump(w io.Writer, format Format) error {
	events := t.Snapshot()

	for _, ev := range events {
		data := FormatEvent(&ev, format)
		if _, err := w.Write(data); err != nil {
			return err
		}
	}

	return nil
}

// Flush is a no-op for RingTracer since everything is in memory.
func (t *RingTracer) Flush() error {
	return nil
}

// Close is a no-op for RingTracer.
func (t *RingTracer) Close() error {
	return nil
}

// Level returns the current tracing level.
func (t *RingTracer) Level() Level {
	return t.level
}

// Enabled returns true if tracing is active.
func (t *RingTracer) Enabled() bool {
	return t.level > LevelOff
}
