# Task 11: Multishard Net Lifecycle Migration

**Status:** Draft
**Kind:** runtime code
**Depends on:** Task 4, Task 8, Task 9, Task 10

## Context

By this point: `N` real shards exist (Task 6), tasks are placed on owner
shards with a no-steal boundary (Task 7), listener/connection objects carry
owner-shard metadata (Task 8), accept distribution actually populates the
right shard's `fd_registry` (Task 9), and every shard has its own poller and
wake mechanism (Task 10). What is still missing is making the *rest* of the
net lifecycle — read/write completion, close, cancellation, shutdown —
consistently resolve through the owner shard's state instead of falling back
to shard 0's, anywhere in the remaining call chain.

The current fd-registry completion functions
(`runtime/native/rt_fd_registry.h:103-108`) take an `rt_executor* ex`
parameter and, per Epic 4's design, resolve the registry through
`rt_executor_fd_registry(ex)` (`rt_runtime.c:152-156`), which is the
shard-0-only accessor:

```c
rt_fd_completion_summary rt_fd_registry_complete_ready_net_waiters(
    rt_executor* ex, const rt_fd_poll_interest* snapshot, int read_ready, int write_ready);
rt_fd_completion_summary rt_fd_registry_drain_shutdown_net_waiters_locked(
    rt_executor* ex, rt_fd_registry* registry);
rt_fd_completion_summary rt_fd_registry_wake_closed_net_waiters(
    rt_executor* ex, const rt_fd_lifecycle_snapshot* snapshot);
```

Every one of these must become shard-aware (either take the owner shard
directly instead of resolving through `ex`, or resolve through the new
shard-indexed accessor from Task 6) — otherwise a connection accepted onto
shard 2 would have its close/cancel/readiness-completion silently handled
against shard 0's registry and waiter store, which is exactly the "implicit
handoff" and "shard-0 fallback" behavior the Accept Ownership Contract and
Task 5's static gate forbid.

At the same time, per the Epic 6 Boundary Decisions paragraph, this task must
*not* touch non-net waiter kinds: channel send/recv, join, scope wake,
cancellation of non-net waits, timer/select waiters, and blocking
completions all keep routing through the existing shard-0
`rt_executor_waiter_store`/`rt_executor_channel_blocking_compat` accessors,
because `rt_shard.waiter_store` (`rt_async_internal.h:158`) is a single
struct type shared by every waiter kind — this task must migrate only the
net-key rows within it to owner-shard resolution, while leaving every other
key kind pointed at shard 0 exactly as before. Task 2's dependency map is the
authoritative list of which call sites are which; do not reclassify a call
site here without updating that map's evidence.

## Goal

Migrate read/write waiter completion, close, cancellation, and shutdown for
net fds to the owner-shard model built by Tasks 6-10, while every non-net
primitive keeps its current global-compatibility semantics unchanged.

## Why This Task Exists

This is where the epic's central promise becomes true end to end: *"Read,
write, close, cancellation, and shutdown for that connection use the owner
shard's registry and waiter state"* and *"Closing a listener or connection
wakes or cancels only waiters owned by the correct shard and does not
complete stale waiters on another shard."* Tasks 6-10 built the pieces; this
task is what wires the remaining call chain through them instead of leaving
a shard-0 fallback quietly load-bearing underneath.

## Scope

- Make `rt_fd_registry_complete_ready_net_waiters`,
  `rt_fd_registry_drain_shutdown_net_waiters_locked`, and
  `rt_fd_registry_wake_closed_net_waiters` (and any other net-owned entry
  point Task 2's map lists) resolve through the accepting connection's
  actual owner shard, not always shard 0.
- Update the per-shard poller loop (Task 10) to call these functions against
  its own shard's registry/scratch only.
- Update close and cancellation paths for a `NetConn`/`NetListener` (Task 8's
  extended structs) to resolve and act on the owner shard's registry and
  waiter store exclusively.
- Update runtime shutdown to drain every shard's net waiters (not just
  shard 0's), using each shard's own wake mechanism from Task 10 to ensure
  no shard is left parked with undrained net state.
- Explicitly verify no non-net waiter kind (channel, join, scope, timer,
  blocking) was accidentally touched by this migration — re-run the full
  `LIVENESS_PROBES.md` non-net waiter suite (cancellation/join/timeout smoke,
  MT correctness channel fixture, sync channel fallback and compensation) to
  prove they are unaffected.
- Close remaining Task 4 pending contract tests that depend on real
  close/cancellation/shutdown routing per owner shard.

## Out Of Scope

- Adding any new waiter kind or changing non-net waiter semantics.
- Cross-shard messaging of any kind — every operation here still happens
  under `ex->lock`, just resolved against a different shard's fields within
  that same lock.
- Migration of an already-placed connection to a different shard (never in
  scope for this epic).

## Approach / Steps

1. Confirm Tasks 4, 8, 9, 10 have landed; re-read Task 2's map for the exact
   net-owned call-site list one more time (things may have shifted slightly
   during Tasks 6-10's implementation — check drift, do not assume the map
   is still perfectly accurate).
2. Change the fd-registry completion function signatures (or add
   shard-explicit variants) so every call site resolves the owner shard
   correctly. Prefer changing the signature to take the shard directly if
   most callers already have it in hand after Task 9/10; add a thin
   shard-0-preserving wrapper only if a genuinely stays-global-compat caller
   still needs one (there should not be one, since these are all net-owned
   functions per Task 2's classification — if you find one, treat it as a
   map correction, not a reason to keep two code paths).
3. Update the per-shard poller (Task 10) call sites.
4. Update close/cancellation call sites for `NetConn`/`NetListener`.
5. Update shutdown to iterate every shard.
6. Run the full non-net waiter liveness suite to confirm no regression.
7. Run Task 4's remaining pending tests; flip to passing or correct with an
   explicit note.
8. Update `06-evidence.md` and `NOTES.md`.

## Files

Touch:

- `runtime/native/rt_fd_registry.c`, `rt_fd_registry.h` (shard-aware
  completion/drain/wake function signatures)
- `runtime/native/rt_net.c` (call sites: poller, close, cancellation)
- `runtime/native/rt_async_state.c` (shutdown path, if it directly calls any
  of the above)

Read:

- `docs/runtime-v2-epics/06-accept-ownership-dependency-map.md` (Task 2,
  re-verify for drift)
- `docs/runtime-v2-epics/06-evidence.md` (Tasks 8, 9, 10 entries)
- `docs/runtime-v2-epics/LIVENESS_PROBES.md` (non-net waiter probe rows, and
  "Timer, timeout, and shutdown liveness" row)

## Skills & Working Practice

- Full Global Rule 9 plan gate: state the exact function-signature changes
  and which call sites they touch before writing code, since this task
  touches the most call sites of any implementation task in the epic.
- This is the highest-blast-radius task for accidentally regressing non-net
  waiters, because they share the same `rt_waiter_store` type. Treat any
  unexpected change in a non-net liveness probe as a stop-the-line signal,
  not a flake to retry past.
- Sequenced after Tasks 4, 8, 9, 10; nothing in the epic can proceed on this
  task's output until it lands, since Task 12 (benchmarks) needs the real
  end-to-end lifecycle to measure.
- `rt_fd_registry.c` is 409 lines, `rt_net.c` is 904 (already over the
  `.loc-legacy-allowlist` ceiling per `RV2-DEBT-004`) — record line-count
  impact for both; this task is a strong candidate to make `rt_net.c` worse
  if care is not taken, which is exactly what Task 14's refactor tranche
  exists to address afterward, but this task should still not gratuitously
  grow it.

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- `go test ./internal/vm -run 'TestRuntimeV2Accept'` (remaining pending
  cases)
- Full non-net waiter liveness suite from `LIVENESS_PROBES.md`
  (cancellation/join/timeout smoke, MT correctness channel fixture, sync
  channel fallback and compensation)
- Native net benchmark (`scripts/bench_native_net.sh`) as a smoke pass ahead
  of Task 12's full evidence
- `git diff --check`
- Sentrux root and scoped scans

## Definition Of Done

- [ ] Every net-owned fd-registry entry point resolves the connection's
      actual owner shard, not shard 0, under `SURGE_SHARDS>1`.
- [ ] Close, cancellation, and shutdown for a connection only touch its
      owner shard's registry and waiter state.
- [ ] Shutdown drains every shard's net waiters, not just shard 0's.
- [ ] Every non-net waiter liveness probe (channel, join, scope, timer,
      blocking) is unaffected — proven by re-running the full suite, not
      assumed.
- [ ] Remaining relevant Task 4 pending tests pass or are corrected with an
      explicit note.
- [ ] Line-count impact on `rt_fd_registry.c` and `rt_net.c` is recorded.

## Evidence To Record

- `06-evidence.md`: Contracts Touched (owner-shard net lifecycle routing,
  shutdown drain), Commands/Checks (full non-net suite results), Files
  Touched with line-count deltas.
- `NOTES.md`: any Task 2 map correction found during this task, and
  confirmation that non-net waiter kinds were re-verified untouched.
