# Task 12: Trace Counters And Benchmark Contract

**Status:** Draft
**Kind:** trace/benchmark

## Goal

Make the registry behavior visible through counters, trace output, and native
net benchmark evidence.

## Scope

- Add or update counters for live entries, registrations, updates, closes,
  cancellations, stale completions, and poll cycles.
- Add trace evidence that covered probes use registry-derived poll input.
- Run before/after native net benchmark reporting for the final registry path.
- Avoid treating counter names as public ABI unless the existing trace contract
  already does.

## Files

- `runtime/native/rt_async_trace.c`
- `runtime/native/rt_net.c`
- fd registry implementation/header files
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- native net benchmark
- trace dump command selected in task detail
- `git diff --check`
- Sentrux root and scoped scans

## Done

- Evidence can prove registry usage without reading code manually.
- Benchmark deltas are recorded and explained.
- Counter additions do not hide regressions in existing trace fields.
