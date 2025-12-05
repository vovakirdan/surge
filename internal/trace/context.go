package trace

import "context"

// ctxKey is the key type for storing Tracer in context.
type ctxKey struct{}

// FromContext extracts the Tracer from context.
// If not found, returns Nop tracer.
func FromContext(ctx context.Context) Tracer {
	if ctx == nil {
		return Nop
	}
	if t, ok := ctx.Value(ctxKey{}).(Tracer); ok {
		return t
	}
	return Nop
}

// WithTracer attaches a Tracer to context.
func WithTracer(ctx context.Context, t Tracer) context.Context {
	if t == nil {
		t = Nop
	}
	return context.WithValue(ctx, ctxKey{}, t)
}

// SpanContext holds current span info for propagation.
type SpanContext struct {
	SpanID uint64
	GID    uint64
}

type spanCtxKey struct{}

// CurrentSpan retrieves the active span context from context.
// Returns zero SpanContext if not found.
func CurrentSpan(ctx context.Context) SpanContext {
	if ctx == nil {
		return SpanContext{}
	}
	if sc, ok := ctx.Value(spanCtxKey{}).(SpanContext); ok {
		return sc
	}
	return SpanContext{}
}

// WithSpanContext attaches span context.
func WithSpanContext(ctx context.Context, sc SpanContext) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, spanCtxKey{}, sc)
}
