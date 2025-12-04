// Package trace provides a tracing subsystem for the Surge compiler.
//
// The trace package enables tracking of compilation phases, module processing,
// and other operations to help diagnose performance issues and hangs.
//
// # Usage
//
// Enable tracing via command-line flags:
//
//	surge diag --trace=- --trace-level=phase myfile.sg
//
// # Architecture
//
// The package provides several tracer implementations:
//
//   - NopTracer: Zero-overhead no-op tracer when disabled
//   - StreamTracer: Immediate write to output (file/stderr)
//   - RingTracer: Circular buffer for crash dumps
//   - MultiTracer: Combines multiple tracers
//
// # Levels
//
// Tracing verbosity is controlled by levels:
//
//   - LevelOff: No tracing
//   - LevelError: Only crash dumps
//   - LevelPhase: Driver and pass boundaries
//   - LevelDetail: Module-level events
//   - LevelDebug: Everything including AST nodes
//
// # Scopes
//
// Events are categorized by scope:
//
//   - ScopeDriver: Top-level CLI operations
//   - ScopeModule: Per-module processing
//   - ScopePass: Compilation phases (lex, parse, sema, borrow)
//   - ScopeNode: AST node level (future)
//
// # Context Propagation
//
// Tracers are propagated through the compilation pipeline via context:
//
//	ctx = trace.WithTracer(ctx, tracer)
//	t := trace.FromContext(ctx)
//
//	span := trace.Begin(t, trace.ScopePass, "parse", parentID)
//	defer span.End("")
package trace
