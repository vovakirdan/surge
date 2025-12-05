package trace

import (
	"bytes"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"
)

var (
	globalSeq   uint64
	globalSpans uint64
)

// NextSeq returns a monotonically increasing sequence number.
func NextSeq() uint64 {
	return atomic.AddUint64(&globalSeq, 1)
}

// NextSpanID returns a unique span ID.
func NextSpanID() uint64 {
	return atomic.AddUint64(&globalSpans, 1)
}

// getGoroutineID extracts the current goroutine ID using runtime.Stack.
// This is a lightweight approach that doesn't require linkname or unsafe.
func getGoroutineID() uint64 {
	buf := make([]byte, 64)
	n := runtime.Stack(buf, false)
	buf = buf[:n]

	// Stack format: "goroutine 123 [running]:\n..."
	// Extract the number between "goroutine " and " ["
	const prefix = "goroutine "
	if !bytes.HasPrefix(buf, []byte(prefix)) {
		return 0
	}

	buf = buf[len(prefix):]
	end := bytes.IndexByte(buf, ' ')
	if end < 0 {
		return 0
	}

	gid, err := strconv.ParseUint(string(buf[:end]), 10, 64)
	if err != nil {
		return 0
	}
	return gid
}

// Span provides a convenient RAII-style span tracking.
type Span struct {
	tracer   Tracer
	id       uint64
	parentID uint64
	gid      uint64
	scope    Scope
	name     string
	started  time.Time
	extra    map[string]string
}

// Begin starts a new span and emits SpanBegin event.
// parent is the parent span ID (0 if root).
func Begin(t Tracer, scope Scope, name string, parent uint64) *Span {
	if t == nil || !t.Enabled() {
		return &Span{tracer: Nop}
	}

	// Check if we should emit at this scope
	if !t.Level().ShouldEmit(scope) {
		return &Span{tracer: Nop}
	}

	id := NextSpanID()
	gid := getGoroutineID()
	now := time.Now()

	t.Emit(&Event{
		Time:     now,
		Seq:      NextSeq(),
		Kind:     KindSpanBegin,
		Scope:    scope,
		SpanID:   id,
		ParentID: parent,
		GID:      gid,
		Name:     name,
	})

	return &Span{
		tracer:   t,
		id:       id,
		parentID: parent,
		gid:      gid,
		scope:    scope,
		name:     name,
		started:  now,
	}
}

// End emits SpanEnd event and returns the duration.
func (s *Span) End(detail string) time.Duration {
	if s == nil || s.tracer == nil || !s.tracer.Enabled() {
		return 0
	}

	dur := time.Since(s.started)

	s.tracer.Emit(&Event{
		Time:     time.Now(),
		Seq:      NextSeq(),
		Kind:     KindSpanEnd,
		Scope:    s.scope,
		SpanID:   s.id,
		ParentID: s.parentID,
		GID:      s.gid,
		Name:     s.name,
		Detail:   detail,
		Extra:    s.extra,
	})

	return dur
}

// WithExtra adds a key-value pair to the end event.
// Returns the span for method chaining.
func (s *Span) WithExtra(key, value string) *Span {
	if s == nil || s.tracer == nil || !s.tracer.Enabled() {
		return s
	}

	if s.extra == nil {
		s.extra = make(map[string]string)
	}
	s.extra[key] = value
	return s
}

// ID returns the span ID.
func (s *Span) ID() uint64 {
	if s == nil {
		return 0
	}
	return s.id
}
