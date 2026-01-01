# Surge Compiler Tracing
[English](TRACING.md) | [Russian](TRACING.ru.md)
> Примечание: этот файл пока не переведен; содержимое совпадает с английской версией.

Surge includes a built-in tracing system to diagnose compiler hangs, performance
issues, and pipeline behavior. Tracing is controlled by global CLI flags and
emits structured events during `surge diag`, `surge parse`, and other commands.

---

## Quick Start

```bash
# High-level phases to stderr
surge diag file.sg --trace=- --trace-level=phase --trace-mode=stream

# Full detail (parser + sema internals) to a file
surge diag file.sg --trace=trace.log --trace-level=debug

# With heartbeat (hang detection)
surge diag file.sg --trace=trace.log --trace-level=debug --trace-heartbeat=1s
```

---

## Flags

Global flags (see `cmd/surge/main.go`):

- `--trace=<path>`: output file (`-` for stderr, empty to disable)
- `--trace-level=off|error|phase|detail|debug`
- `--trace-mode=stream|ring|both` (default: `ring`)
- `--trace-format=auto|text|ndjson|chrome`
- `--trace-ring-size=<n>` (default: 4096)
- `--trace-heartbeat=<duration>` (0 disables)

**Auto behavior:**

- If `--trace` is a file path and `--trace-mode=ring`, the mode is
  auto-switched to **stream**. To force ring, explicitly set
  `--trace-mode=ring`.
- `--trace-format=auto` detects format from extension:
  - `.ndjson` => NDJSON
  - `.json` or `.chrome.json` => Chrome trace
  - otherwise => text

---

## Trace Levels

| Level | Emits | Notes |
|-------|-------|-------|
| `off` | nothing | tracing disabled |
| `error` | no spans (reserved) | only heartbeat + crash dump plumbing |
| `phase` | driver + pass spans | high-level pipeline |
| `detail` | + module spans | module resolution + graph |
| `debug` | + node spans | parser + sema internals |

`error` currently does not emit spans; use `phase` or higher for real output.

---

## Trace Modes

### Stream

Writes events immediately to the output.

```bash
surge diag file.sg --trace=trace.log --trace-level=detail --trace-mode=stream
```

### Ring (default)

Keeps the last N events in memory (circular buffer). No output is written
unless you explicitly dump it.

```bash
surge diag file.sg --trace-level=detail --trace-mode=ring
```

If you set `--trace` while forcing ring mode, the ring buffer is **dumped on
panic or SIGINT** into:

```
<path>.panic.trace
<path>.interrupt.trace
```

Dump format is always **text**.

### Both

Sends events to both stream and ring:

```bash
surge diag file.sg --trace=trace.log --trace-level=debug --trace-mode=both
```

---

## Output Formats

### Text (human-readable)

Format: `[seq NNNNNN] <indent><event> name (detail) {extra=...}`

Example:

```
[seq      1] → diagnose
[seq      2]   → tokenize
[seq      3]   ← tokenize (diags=0)
[seq      4] → parse
[seq      5] ← parse (items=12)
[seq      6] • parse_items_progress (item=100)
[seq      7] ♡ heartbeat (#1)
```

Legend:

- `→` span begin
- `←` span end
- `•` point event
- `♡` heartbeat

Indentation is a single level when a parent span exists.

### NDJSON

```bash
surge diag file.sg --trace=trace.ndjson --trace-level=debug --trace-format=ndjson
```

Each line is a JSON object:

```json
{"time":"2025-12-05T12:00:00.123456Z","seq":1,"kind":"begin","scope":"pass","span_id":42,"parent_id":0,"gid":1,"name":"parse"}
```

Fields:

- `time`, `seq`, `kind`, `scope`
- `span_id`, `parent_id`, `gid`
- `name`, `detail`, `extra`

### Chrome Trace

```bash
surge diag file.sg --trace=trace.json --trace-level=detail --trace-format=chrome
```

Open `chrome://tracing` and load the JSON file. The stream writer produces a
`traceEvents` array compatible with the Chrome trace viewer.

---

## Heartbeat

`--trace-heartbeat=1s` emits periodic `heartbeat` events. This is useful for
identifying hangs: heartbeats continue while work stops.

Example:

```
[seq      1] → parse
[seq      2] ♡ heartbeat (#1)
[seq      3] ♡ heartbeat (#2)
# no new spans -> likely hang in parse
```

---

## Instrumented Components (v1)

Common spans include:

- Driver phases: `diagnose`, `load_file`, `tokenize`, `parse`, `symbols`, `sema`
- Module graph: `parse_module_dir`, `analyze_dependency`, `process_module`
- Parser nodes (debug): `parse_items`, `parse_block`, `parse_binary_expr`, `parse_postfix_expr`
- Sema internals (debug): `sema_check`, `walk_item`, `walk_stmt`, `type_expr`,
  `call_result_type`, `check_contract_satisfaction`, `methods_for_type`
- HIR analysis (when HIR is built): `hir_build_borrow_graph`, `hir_build_move_plan`

---

## Performance Notes

- `phase` has very low overhead and is safe for regular use.
- `debug` can be expensive (parser + sema spans per node).
- `ring` reduces I/O overhead but does not write output unless dumped.

---

## Related Tracing Flags

Separate from compiler tracing:

- `--runtime-trace=<file>`: Go runtime trace
- `surge run --vm-trace`: VM execution tracing

These are **not** part of the compiler trace stream.
