# Surge Compiler Tracing

Surge compiler includes a built-in tracing system for debugging compiler hangs, performance issues, and understanding compiler behavior.

## Quick Start

```bash
# Basic tracing (phases only)
surge diag --trace=trace.log --trace-level=phase file.sg

# Full detail tracing (includes parser internals)
surge diag --trace=trace.log --trace-level=debug file.sg

# With heartbeat for hang detection
surge diag --trace=trace.log --trace-level=debug --trace-heartbeat=1s file.sg
```

## Trace Levels

The tracing system supports multiple detail levels, from high-level phases to low-level parser operations:

| Level | Scopes Included | Use Case | Overhead |
|-------|----------------|----------|----------|
| `off` | None | Production use | ~0% |
| `error` | Error events only | Crash debugging | Minimal |
| `phase` | Driver, Pass | High-level pipeline view | Low |
| `detail` | + Module | Module resolution debugging | Low-Medium |
| `debug` | + Node | Parser/AST debugging, hang detection | Medium-High |

### Scope Hierarchy

- **Driver** (`ScopeDriver`): Top-level commands (diag, build, parse)
- **Pass** (`ScopePass`): Compilation phases (tokenize, parse, sema, borrow)
- **Module** (`ScopeModule`): Per-module operations (parse_module_dir, resolve_module)
- **Node** (`ScopeNode`): AST-level operations (parse_items, parse_block, typeExpr)

## Trace Modes

### Stream Mode

Writes events immediately to output. Use for:
- Live debugging
- Long-running compilations
- When you need to see progress in real-time

```bash
surge diag --trace=trace.log --trace-mode=stream --trace-level=debug file.sg
```

**Pros:** Immediate visibility, no data loss on crashes
**Cons:** Higher I/O overhead

### Ring Mode (Default)

Keeps last N events in a circular buffer. Use for:
- Post-mortem debugging after crashes
- Reducing I/O overhead
- Memory-efficient tracing

```bash
surge diag --trace=trace.log --trace-mode=ring --trace-ring-size=8192 file.sg
```

**Pros:** Low overhead, captures last events before crash
**Cons:** Limited history, events can be overwritten

### Both Mode

Combines stream and ring modes. Use for:
- Maximum information capture
- Critical debugging sessions

```bash
surge diag --trace=trace.log --trace-mode=both file.sg
```

## Heartbeat Mechanism

Heartbeat emits periodic events to detect compiler hangs. If the compiler hangs, heartbeat continues while actual work stops, making it easy to identify the hang location.

```bash
# Emit heartbeat every second
surge diag --trace=trace.log --trace-level=phase --trace-heartbeat=1s file.sg
```

In the trace output:
```
[  1.000ms] ♥ heartbeat#1
[  2.000ms] ♥ heartbeat#2
[  3.000ms] ♥ heartbeat#3
  → parse_items        # Started parsing
  (no activity)        # Compiler hung here!
[  4.000ms] ♥ heartbeat#4  # Heartbeat continues
[  5.000ms] ♥ heartbeat#5
```

## Instrumented Components

The tracing system instruments all major compiler subsystems:

### Heartbeat
- Background goroutine emitting periodic events
- Continues even if compiler hangs
- Configurable interval via `--trace-heartbeat`
- Helps identify hang location by showing last activity before silence

### Module System (Detail level)
- `parse_module_dir` - Parsing all files in a module directory
- `analyze_dependency` - Analyzing imported modules
- `process_module` - Processing module in dependency graph
- `load_std_module` - Loading standard library modules
- `resolve_module_record` - Symbol resolution for modules

### Parser (Debug level)
- `parse_items` - Top-level item parsing with progress points every 100 items
- `parse_block` - Block parsing with statement counting
- `parse_binary_expr` - Binary expression parsing with depth limiting (≤20)
- `parse_postfix_expr` - Postfix expression parsing with iteration tracking
- `resync_top` - Error recovery with token skip counting

### Semantic Analysis (Debug level)
Core analysis with 7 internal phases:
- `build_magic_index` - Magic method index construction
- `ensure_builtin_magic` - Builtin magic methods setup
- `build_scope_index` - Scope hierarchy construction
- `build_symbol_index` - Symbol table indexing
- `build_export_indexes` - Module export tracking
- `register_types` - Type registration
- `flush_borrow` - Borrow checker finalization

Per-item and statement analysis:
- `walk_item` - Per-item semantic checks (Detail level)
- `walk_stmt` - Statement-level checks (Debug level)
- `type_expr` - Expression type inference with depth limiting (≤20)

Complex operations:
- `call_result_type` - Function call resolution with overload selection
  - Tracks: argument count, candidate count
  - Example: `{args=1, candidates=2}`
- `check_contract_satisfaction` - Contract compliance checking
  - Tracks: type name, fields checked, methods checked
  - Example: `{type=Erring<T, E>, fields_checked=2, methods_checked=0}`
- `methods_for_type` - Method resolution for types (O(n²) complexity)
  - Tracks: type name, methods found
- `instantiate_type` - Generic type instantiation with caching
  - Tracks: type arguments count, cache hits
  - Example: `{args=1, cached=true}`

## Trace Output Format

### Text Format (Human-Readable)

```
[  0.002ms] → diagnose
[  0.004ms]   → load_file
[  0.006ms]   ← load_file
[  0.008ms]   → tokenize
[  0.010ms]   ← tokenize
[  0.012ms]   → parse
[  0.014ms] → parse_items
[  0.016ms] → parse_block
[  0.018ms] ← parse_block (stmts=3)
[  0.196ms] ← parse_items
[  0.198ms]   ← parse
[  0.415ms] • parse_items_progress (item=100)
```

- `→` - Span begin
- `←` - Span end (with details)
- `•` - Point event (progress marker)
- `♥` - Heartbeat event
- Indentation shows nesting level
- Timestamps in milliseconds from start
- Extra details in parentheses or braces

### NDJSON Format (Machine-Readable)

```bash
surge diag --trace=trace.ndjson --trace-format=ndjson --trace-level=debug file.sg
```

Each line is a JSON object:
```json
{"time":"2025-12-05T12:00:00.123Z","seq":1,"kind":"span_begin","scope":"pass","span_id":42,"parent_id":0,"name":"parse","gid":1}
{"time":"2025-12-05T12:00:00.125Z","seq":2,"kind":"span_end","scope":"pass","span_id":42,"name":"parse","detail":"items=15","gid":1}
```

## Performance Impact

**Benchmark setup:** stdlib/saturating_cast.sg, 10 runs averaged, measured on macOS (baseline: 11ms)

| Configuration | Time | Overhead | Notes |
|--------------|------|----------|-------|
| `--trace-level=off` | 11ms | 0% | Baseline - nil checks only, essentially free |
| `--trace-level=phase --trace-mode=stream` | 11ms | ~0% | High-level pipeline spans only |
| `--trace-level=phase --trace-mode=ring` | 11ms | ~0% | Ring buffer has no measurable overhead |
| `--trace-level=detail --trace-mode=stream` | 13ms | +18% | Module-level spans + I/O |
| `--trace-level=detail --trace-mode=ring` | 12ms | +9% | Module-level spans, ring buffer |
| `--trace-level=debug --trace-mode=stream` | 67ms | +509% | Full AST instrumentation + I/O |
| `--trace-level=debug --trace-mode=ring` | 56ms | +409% | Full AST instrumentation, ring buffer |

**Key findings:**
- **Phase level**: Negligible overhead (~0%), safe for production use
- **Detail level**: Low overhead (9-18%), acceptable for debugging module issues
- **Debug level**: High overhead (400-500%), use only when debugging parser/sema hangs
- **Ring vs Stream**: Ring mode is ~10% faster than stream mode at debug level
- **Heartbeat**: <1% overhead (not shown, measured separately)

**Note:** Actual overhead depends on:
- Code complexity - more expressions/statements = more spans at debug level
- Parse errors - error recovery adds tracing overhead
- I/O speed - stream mode overhead varies with disk performance
- Ring buffer size - larger buffers slightly increase memory usage

### Minimizing Overhead

1. **Use appropriate level:** Don't use `debug` unless you need parser details
2. **Use ring mode:** Lower I/O overhead than stream mode
3. **Disable when not needed:** `--trace-level=off` (or omit `--trace` flag)
4. **Limit ring size:** Smaller buffer = less memory/CPU

```bash
# Low-overhead configuration for production monitoring
surge diag --trace=trace.log --trace-level=phase --trace-mode=ring --trace-ring-size=1024
```

## Common Use Cases

### Debugging Compiler Hangs

```bash
# Use heartbeat + debug level + ring mode
surge diag --trace=hang.log --trace-level=debug \
           --trace-heartbeat=1s --trace-mode=ring \
           --trace-ring-size=8192 problematic.sg

# After hang, check the trace for last operations before hang
grep -A 10 -B 10 "heartbeat" hang.log | tail -30
```

### Profiling Compilation Performance

```bash
# Phase level is sufficient for high-level profiling
surge diag --trace=profile.log --trace-level=phase \
           --trace-mode=stream large_project/

# Analyze time spent in each phase
grep "←" profile.log | sort -k2 -rn | head -20
```

### Understanding Module Resolution

```bash
# Detail level shows module operations
surge diag --trace=modules.log --trace-level=detail \
           --trace-mode=stream project/

# Filter for module operations
grep -E "(parse_module|resolve_module|analyze_dependency)" modules.log
```

### Finding Parser Bottlenecks

```bash
# Debug level required for parser internals
surge diag --trace=parser.log --trace-level=debug \
           --trace-mode=stream complex_file.sg

# Count parser operations
grep -oE "parse_[a-z_]+" parser.log | sort | uniq -c | sort -rn
```

### Post-Mortem Crash Analysis

```bash
# Ring mode with large buffer captures last events
surge diag --trace=crash.log --trace-level=debug \
           --trace-mode=ring --trace-ring-size=16384 \
           crashing_file.sg || true

# Examine last events before crash
tail -100 crash.log
```

## Trace Analysis Tips

### Finding Performance Bottlenecks

```bash
# Extract spans with duration
grep "←" trace.log | awk '{print $1, $2, $3}' | sort -k1 -rn

# Find slowest operations
grep "parse_binary_expr" trace.log | wc -l  # Count recursive depth
```

### Identifying Infinite Loops

```bash
# Look for warning events in postfix expression parsing
grep "postfix_loop_warning" trace.log

# Check iteration counts
grep "iterations=" trace.log | awk -F'iterations=' '{print $2}' | sed 's/).*$//' | sort -rn | head -10
```

### Progress Tracking

```bash
# Watch progress points in real-time
tail -f trace.log | grep "progress"

# Count completed items
grep "parse_items_progress" trace.log | tail -1
```

## Integration with Other Tools

### ChromeTrace Viewer (Future)

Export to Chrome's trace viewer format:
```bash
surge diag --trace=trace.json --trace-format=chrome file.sg
# Open chrome://tracing and load trace.json
```

### Custom Analysis Scripts

NDJSON format is easily parsed:
```python
import json

with open('trace.ndjson') as f:
    for line in f:
        event = json.loads(line)
        if event['kind'] == 'span_begin':
            print(f"Started {event['name']} at {event['time']}")
```

## Environment Variables

- `SURGE_TRACE_OUTPUT` - Default trace output path
- `SURGE_TRACE_LEVEL` - Default trace level
- `SURGE_TRACE_MODE` - Default trace mode

```bash
export SURGE_TRACE_OUTPUT=surge.trace
export SURGE_TRACE_LEVEL=phase
surge diag file.sg  # Uses defaults from environment
```

## Crash Safety

The tracing system preserves diagnostic data even when compilation is interrupted or crashes.

### Signal Handling (SIGINT/SIGTERM)

When you interrupt compilation with Ctrl+C or send SIGTERM:
- Ring buffer is dumped to `<output>.interrupt.log`
- Heartbeat is stopped gracefully
- Tracer is flushed and closed
- Process exits with appropriate code (130 for SIGINT, 143 for SIGTERM)

```bash
# Start compilation
surge diag --trace=trace.log --trace-mode=ring large_project/ &
SURGE_PID=$!

# Interrupt it
kill -INT $SURGE_PID

# Check dump file
ls -lh trace.interrupt.log
```

### Panic Recovery

If the compiler panics:
- `defer dumpTraceOnPanic()` catches the panic
- Ring buffer is dumped to `<output>.panic.log`
- Tracer is flushed and closed
- Panic is re-raised to maintain normal panic behavior

This preserves the last N events before the crash, making it easier to diagnose what went wrong.

### Dump File Naming

- `trace.log` → `trace.interrupt.log` (on SIGINT/SIGTERM)
- `trace.log` → `trace.panic.log` (on panic)
- `-` (stderr) → `surge.interrupt.trace` or `surge.panic.trace`

### Testing Crash Safety

Manual testing recommended due to compiler speed:
1. **SIGINT test**: Start compilation on large project with `--trace-mode=ring`, send SIGINT, verify `.interrupt.log` created
2. **Panic test**: Modify source to add `panic("test")`, verify `.panic.log` created
3. **Exit codes**: Verify 130 for SIGINT, 143 for SIGTERM

## Limitations

1. **No distributed tracing** - Single process only
2. **Text format only** - NDJSON and Chrome formats planned
3. **No filtering** - All events at selected level are captured
4. **Limited aggregation** - No built-in statistics or summaries
5. **No sampling** - All events captured (except depth/iteration limits)

## Features

### Implemented

**Core Instrumentation:**
- Heartbeat mechanism for hang detection
- Module system tracing (parse, analyze, resolve)
- Parser instrumentation (items, blocks, expressions)
- Semantic analysis tracing (7 internal phases)
- Complex sema operations (calls, contracts, instantiation)

**Crash Safety:**
- Signal handling (SIGINT/SIGTERM) with ring buffer dump
- Panic recovery with trace preservation
- MultiTracer support for extracting RingTracer

**Output Modes:**
- Stream mode - immediate I/O
- Ring mode - circular buffer
- Both mode - combines stream and ring

**Performance:**
- Zero overhead when disabled (nil checks only)
- ~0% overhead at phase level
- 9-18% overhead at detail level
- 409-509% overhead at debug level

### Planned

- Chrome Trace Viewer format export (trace_events JSON)
- NDJSON format support for machine-readable output
- Sampling mode for lower overhead in production
- Built-in trace analysis tools (statistics, bottleneck detection)
- Distributed tracing for parallel compilation
- WebUI for interactive trace visualization
- Flamegraph generation from trace data

## Implementation Details

The tracing system is designed for:
- **Zero overhead when disabled** - Nil checks and early returns
- **Minimal allocations** - Event pooling and reuse
- **Thread-safe** - All operations safe for concurrent use
- **Non-blocking** - Ring mode never blocks on I/O
- **Crash-resilient** - Ring buffer preserved on panics

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Application                          │
│  (driver, parser, sema with trace.FromContext)             │
└─────────────────────┬───────────────────────────────────────┘
                      │
                      ▼
            ┌─────────────────────┐
            │   Tracer Interface  │
            │  (Level, Emit, ...)  │
            └──────────┬───────────┘
                      │
        ┌─────────────┼─────────────┬──────────────┐
        ▼             ▼             ▼              ▼
   ┌────────┐   ┌─────────┐   ┌────────┐   ┌──────────┐
   │  Nop   │   │ Stream  │   │  Ring  │   │  Multi   │
   │ Tracer │   │ Tracer  │   │ Tracer │   │  Tracer  │
   └────────┘   └────┬────┘   └───┬────┘   └────┬─────┘
                     │            │              │
                     ▼            ▼              ▼
              ┌────────────┐  ┌────────┐  (Stream+Ring)
              │ File/Stdio │  │ Buffer │
              └────────────┘  └────────┘
```

### Span Lifecycle

1. `trace.Begin()` - Creates span, emits SpanBegin event
2. `span.WithExtra()` - Adds metadata to span (optional)
3. `span.End()` - Emits SpanEnd event with detail string

### Event Types

- `KindSpanBegin` - Function/operation started
- `KindSpanEnd` - Function/operation completed
- `KindPoint` - Progress marker or milestone
- `KindHeartbeat` - Periodic liveness indicator

---

### Source Files

**Core Infrastructure:**
- `internal/trace/tracer.go` - Tracer interface and implementations
- `internal/trace/heartbeat.go` - Heartbeat mechanism
- `internal/trace/span.go` - Span lifecycle management
- `internal/trace/multi.go` - MultiTracer with Tracers() accessor

**CLI Integration:**
- `cmd/surge/trace_setup.go` - Tracer initialization and crash safety
  - Signal handling (SIGINT/SIGTERM)
  - Panic recovery with defer
  - Ring buffer dump on interruption
  - findRingTracer() for MultiTracer support
- `cmd/surge/diagnose.go` - Driver instrumentation with panic recovery

**Compiler Instrumentation:**
- `internal/driver/diagnose_modules.go` - Module system tracing
- `internal/parser/parser.go` - Parser spans and progress
- `internal/parser/expression.go` - Expression parsing
- `internal/parser/stmt_parser.go` - Statement parsing
- `internal/sema/check.go` - Sema entry point with context
- `internal/sema/type_checker_core.go` - Core sema phases and walk functions
- `internal/sema/type_expr.go` - Expression type inference
- `internal/sema/type_expr_calls.go` - Function call resolution
- `internal/sema/contract_match.go` - Contract checking and method resolution
- `internal/sema/type_decl_instantiate.go` - Generic type instantiation
