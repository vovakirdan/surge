# Task 12: Trace Counters And Benchmark Evidence

**Status:** Draft
**Kind:** trace/benchmark
**Depends on:** Task 9, Task 10, Task 11

## Context

By this task, accept ownership, per-shard polling, and net lifecycle
migration are all real (Tasks 9-11). The epic is explicitly performance-
sensitive and states it "must not close with only functional tests" —
this task is that required evidence.

Existing trace infrastructure to extend, not replace: `TRACE_NET` fields
(`io_poll_calls`, `io_poll_net_ready`, `io_waiter_scan_entries`,
`io_poll_dedup_checks`, etc. — full list in `LIVENESS_PROBES.md` "Trace
Fields To Record") and `SCHED_TRACE` fields (`mode`, `seed`, `local`,
`inject`, `steal`, `events`, `hash`) already exist. This task adds shard-aware
fields to both families rather than inventing a parallel trace format.

Critically, the epic's own boundary decision means the benchmark story here
is deliberately not "faster is better": *"Because Epic 6 keeps the global
executor lock, a flat or disappointing throughput result is acceptable if the
evidence proves correct owner placement, reduced or eliminated
connection-task steals, and no regression in the stable small-load rows."*
This task's job is to produce evidence that lets a reader judge *ownership
correctness and locality*, not just a throughput number — a naive read of
"multi-shard didn't get faster" would be a false negative if taken alone.

`RUNTIME_V2.md`'s Benchmark Plan section is explicit that the existing
32-connection probe is not enough for shared-nothing scaling judgment: with
8 shards, 32 connections means about 4 per shard under perfect distribution,
and `SO_REUSEPORT` "can be skewed at low connection counts." The epic's own
Performance Contract already requires a row near 1k connections (and 10k if
safe) specifically to address this.

## Goal

Add trace counters for shard-aware accept ownership and no-steal behavior,
and produce benchmark evidence comparing single-shard and multi-shard native
TCP rows, explained against the preserved global-lock boundary.

## Why This Task Exists

Without this task, Tasks 9-11's correctness is provable only by targeted
tests, not by the kind of evidence the epic's Performance Contract and
Accept Ownership Contract both require as closeout gates. Trace counters are
also what let Task 4's harder-to-assert contract cases (e.g. "connection
tasks are not reported through `SCHED_TRACE steal`", "no global-path fallback
occurred") become mechanically checkable instead of only visually inspected.

## Scope

- Add trace counters (in `TRACE_NET`/`SCHED_TRACE` or a small new shard-scope
  trace line, whichever fits the existing dump format best) for:
  - shard count in effect for the run;
  - accepted connections by shard (a per-shard count, to observe
    distribution and skew);
  - connection-task owner placement (confirming tasks ran on their
    connection's owner shard);
  - denied or avoided Tier 1 steals (a counter that increments exactly when
    Task 7's no-steal check actually blocked a cross-shard steal attempt, so
    "the boundary exists and did something" is observable, not merely
    "steal count is zero because nothing tried");
  - global-path/shard-0-fallback usage (should read zero for net-owned
    paths under `SURGE_SHARDS>1`, per Task 5's static gate — this counter is
    the runtime-side confirmation of that same invariant);
  - fd readiness batches (how many fds were serviced per poll cycle, per
    shard);
  - shard imbalance (a simple max-minus-min or stddev over the per-shard
    accepted-connection counts).
- Extend `scripts/bench_native_net.sh` (or add a sibling script/mode) to run
  with a configurable `SURGE_SHARDS` and report the new counters alongside
  existing latency/throughput numbers.
- Produce the required benchmark rows:
  - single-shard (`SURGE_SHARDS=1`) and multi-shard rows for 1, 8, and 32
    connections;
  - at least one row near 1k connections, and 10k if the harness can run it
    safely (check `RV2-DEBT-006`/`RV2-DEBT-012` for known benchmark-harness
    limits before assuming 10k is safe; record explicitly if it is skipped
    and why);
  - use the current-checkout `surge` binary for every row (the recorded
    Epic-4-era mistake of benchmarking a stale binary must not repeat here —
    verify the binary's build timestamp/hash matches the current checkout
    before recording a row).
- Explain, in prose next to the numbers, any small-load latency regression
  and any many-connection throughput change (improvement or lack of
  improvement) in terms of the global-lock boundary the epic accepted, not
  in terms of an implied but unproven mechanism.
- Explicitly note expected `SO_REUSEPORT` skew at 1/8/32 connections as
  non-failure, per `RUNTIME_V2.md`'s Benchmark Plan, and judge shard
  distribution from the higher-load row instead.

## Out Of Scope

- CI wiring (Task 13) — this task produces the evidence and the counters;
  Task 13 decides which subset becomes a required gate.
- Any new runtime behavior change — this task only measures and instruments
  Tasks 9-11's already-landed behavior. If a counter reveals a bug, file it
  as a fix for the owning task (9, 10, or 11) rather than quietly patching
  it inside this trace/benchmark task.

## Approach / Steps

1. Confirm Tasks 9, 10, 11 have landed.
2. Design the new counter set; check it fits the existing `TRACE_NET`/
   `SCHED_TRACE` dump format before inventing a new one (Global Rule 5:
   reuse before new machinery).
3. Add the counters to the native runtime; add corresponding Go-side
   assertions where Task 4's contract tests can now use them instead of
   indirect inference.
4. Extend the benchmark script(s) for `SURGE_SHARDS` configurability and
   counter reporting.
5. Verify the benchmark binary is the current checkout before running any
   row (build fresh, check hash/timestamp).
6. Run and record every required row; write the explanation prose next to
   each surprising or notable result.
7. Update `06-evidence.md` and `NOTES.md`.

## Files

Touch:

- `runtime/native/rt_async_state.c` and/or `rt_net.c` (new trace counters)
- `scripts/bench_native_net.sh` (or a new sibling script)

Read:

- `docs/runtime-v2-epics/06-n2-accept-ownership-and-tier1-scheduler.md`
  (Performance Contract)
- `docs/RUNTIME_V2.md` (Benchmark Plan section)
- `docs/runtime-v2-epics/LIVENESS_PROBES.md` ("Trace Fields To Record",
  "Native net benchmark trace script" row)
- `docs/runtime-v2-epics/DEBT.md` (`RV2-DEBT-006`, `RV2-DEBT-012`)

## Skills & Working Practice

- Global Rule 9 plan gate applies for the native counter additions (runtime
  code); the benchmark script extension can proceed more informally since it
  is tooling, not runtime behavior, but should still be reviewed before
  being treated as final evidence.
- Wrap every benchmark invocation in an outer `timeout`, per the accepted
  `RV2-DEBT-006` pattern, until the script owns per-probe timeouts itself.
- Do not let "the number didn't improve" become an unexplained result. The
  epic explicitly accepts flat/disappointing throughput as long as ownership
  and locality evidence is present — if you cannot explain a result this
  way, that is a sign more counters or a repeat run are needed, not that the
  contract failed silently.
- May proceed in parallel with the tail of Task 11 only if write sets stay
  disjoint (trace-counter additions vs. net-lifecycle C code); coordinate
  closely since both touch `rt_net.c`.

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- `timeout 300s env SURGE=<current-checkout-binary> ./scripts/bench_native_net.sh` (or the
  extended multi-shard variant)
- `git diff --check`
- Sentrux root, `runtime/`, and `runtime/native/` scans (this task touches
  `runtime/native/rt_async_state.c`/`rt_net.c`, so it is a runtime-code task
  under `RULES.md` Global Rule 3 and cannot close without recorded Sentrux
  evidence — the benchmark-script-only parts of this task do not exempt the
  native counter additions from this gate)

## Definition Of Done

- [ ] Shard-aware trace counters exist for: shard count, accepted
      connections by shard, connection-task owner placement, denied/avoided
      Tier 1 steals, global-path fallback usage, fd readiness batches, shard
      imbalance.
- [ ] Benchmark rows exist for single-shard and multi-shard at 1, 8, 32
      connections and at least one row near 1k (10k if safe, with an
      explicit skip reason if not).
- [ ] Every row uses a verified current-checkout binary.
- [ ] Every notable result (regression, flat throughput, skew) has an
      explanation grounded in the preserved global-lock boundary or
      `SO_REUSEPORT` skew expectations, not left as an unexplained number.
- [ ] Global-path/shard-0-fallback counter reads zero for net-owned paths
      under `SURGE_SHARDS>1`, matching Task 5's static gate.
- [ ] Sentrux root, `runtime/`, and `runtime/native/` scans are recorded
      pass/fail with `quality_signal`, following `SENTRUX_POLICY.md`'s
      required call order; a dropped scoped `quality_signal` is either
      explained as an accepted proving-spike exception or blocks closing this
      task.

## Evidence To Record

- `06-evidence.md`: Benchmarks And Generated Reports (full row table),
  Trace Counters/Liveness Proof (new counters with observed values),
  Commands/Checks, Sentrux Root/Scoped Signals (before/after this task's
  native counter changes).
- `NOTES.md`: any surprising result and its explanation, and any benchmark
  row explicitly skipped (e.g. 10k connections) with the reason.
