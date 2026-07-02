# Task 12: Trace Counters And Benchmark Contract

**Status:** Complete
**Kind:** trace/benchmark

## Goal

Make the registry behavior visible through counters, trace output, and native
net benchmark evidence.

## Scope

- Do not add runtime counters unless an existing proof gap requires them.
- Keep the current `TRACE_NET` fields as migration/debug evidence, not public
  ABI.
- Add trace evidence that covered probes use registry-derived poll input.
- Expand native net benchmark reporting so existing `TRACE_NET` fields missing
  from the report are copied into the benchmark table.
- Run native net benchmark reporting for the final registry path when feasible.

## Decision

No new runtime counters are added in this task. Existing `TRACE_NET` fields
already answer the acceptance questions:

- `io_waiter_scan_entries`, `io_waiter_net_entries`, and
  `io_poll_dedup_checks` staying zero prove the legacy waiter-derived poll
  rebuild path is unused.
- `io_poll_calls`, `io_poll_rebuilds`, `io_poll_waiters_last`,
  `io_poll_waiters_max`, and `io_poll_waiters_total` expose registry-derived
  poll cycles and snapshot sizes.
- `io_poll_wake_fd`, `io_poll_net_ready`, `io_waiter_complete_calls`, and
  `io_waiter_completed` expose poll wake and completion behavior.

Registration, update, close, cancellation, and stale-completion counters would
be migration-local telemetry today. Adding them now would make unstable names
look like a trace ABI while Task 8-11 already prove those behaviors with
focused tests.

## Files

- `scripts/bench_native_net.sh`
- `internal/vm/runtime_v2_net_waiter_contract_test.go`
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`

## Checks

- focused trace contract test
- focused fd-registry wake/cancel regression test
- native net benchmark
- `git diff --check`
- Heavy gates (`make c-check`, `make cppcheck`, `make runtime-v2-check`,
  `make check`, and Sentrux root/scoped gates) stay owned by the main session
  before the task commit.

## Done

- Evidence can prove registry usage without reading code manually.
- Benchmark trace rows are recorded and explained.
- Counter names are documented as migration/debug evidence, not public ABI.
- No new runtime counters hide regressions in existing trace fields.
