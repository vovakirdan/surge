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

### Phase 1: Heartbeat
- Background goroutine emitting periodic events
- Continues even if compiler hangs
- Configurable interval via `--trace-heartbeat`

### Phase 2: Module System
- `parse_module_dir` - Parsing all files in a module directory
- `analyze_dependency` - Analyzing imported modules
- `process_module` - Processing module in dependency graph
- `load_std_module` - Loading standard library modules
- `resolve_module_record` - Symbol resolution for modules

### Phase 3: Parser (Debug Level)
- `parse_items` - Top-level item parsing with progress points every 100 items
- `parse_block` - Block parsing with statement counting
- `parse_binary_expr` - Binary expression parsing with depth limiting (≤20)
- `parse_postfix_expr` - Postfix expression parsing with iteration tracking
- `resync_top` - Error recovery with token skip counting

### Phase 4: Semantic Analysis (Planned)
- `sema_check` - Overall semantic analysis phases
- `walk_item` - Per-item semantic checks
- `type_expr` - Expression type inference with depth limiting
- `check_contract_satisfaction` - Contract checking
- `instantiate_type` - Generic type instantiation

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

| Configuration | Expected Overhead | Notes |
|--------------|-------------------|-------|
| `--trace-level=off` | ~0% | Nil checks only, essentially free |
| `--trace-level=phase` | <1% | Minimal span creation |
| `--trace-level=detail` | 1-3% | Module-level spans |
| `--trace-level=debug` | 3-10% | Full instrumentation, depends on code complexity |
| `--trace-mode=stream` | +1-2% I/O | Additional disk writes |
| `--trace-mode=ring` | +0.5% | Memory buffer only |
| `--trace-heartbeat=1s` | <0.1% | Background goroutine |

**Note:** Overhead percentages are estimates based on typical workloads. Actual overhead depends on:
- Code complexity (more expressions = more spans)
- Parse errors (error recovery tracing)
- I/O speed (for stream mode)
- Ring buffer size

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

## Limitations

1. **No distributed tracing** - Single process only
2. **Text format only** - NDJSON and Chrome formats planned
3. **No filtering** - All events at selected level are captured
4. **Limited aggregation** - No built-in statistics or summaries
5. **No sampling** - All events captured (except depth/iteration limits)

## Roadmap

- [ ] Phase 4: Sema instrumentation (type checking, contract checking)
- [ ] Phase 5: Deep Sema (instantiation, method resolution)
- [ ] Phase 6: Crash safety improvements (signal handling, panic recovery)
- [ ] Chrome Trace Viewer format export
- [ ] Sampling mode for lower overhead
- [ ] Built-in trace analysis tools
- [ ] Distributed tracing for parallel compilation
- [ ] WebUI for trace visualization

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

For implementation details, see:
- `internal/trace/` - Core tracing infrastructure
- `cmd/surge/trace_setup.go` - CLI integration
- `internal/parser/parser.go` - Parser instrumentation
- `internal/driver/diagnose.go` - Driver instrumentation
