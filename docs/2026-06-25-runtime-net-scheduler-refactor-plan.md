# Runtime net/scheduler refactor plan

Date: 2026-06-25

Scope: native C runtime only. No application-level surgekv changes in this plan.

## Baseline state

- Baseline Surge repo: `08c5fef29bed6bd2df388a9e3ce114fabc55e318`
- Current runtime branch base: `3757b874294c9e93d7de1119b73e981a7cbb4ce2`
- Surge binary: `surge 0.1.13-dev`
- Main hygiene committed before runtime edits:
  - `2d25324d chore(release): package multi-platform assets`
  - `3757b874 docs: remove shipped stdlib bytes spec`
- surgekv probe branch: `codex/runtime-scheduling-probe`
- surgekv probe report: `surgekv/benchmarks/runtime-scheduling-probe.md`
- Root native reports:
  - `build/benchmarks/native-channel-request-reply.md`
  - `build/benchmarks/native-net-request-reply.md`

## Baseline numbers

### surgekv runtime probe

Pure channel request/reply:

| mode | reused reply | new reply |
| --- | ---: | ---: |
| `SURGE_THREADS=1` | 3744 ns/op | 4194 ns/op |
| `SURGE_THREADS=8` | 10535 ns/op | 11480 ns/op |
| `SURGE_THREADS=8 SURGE_CHANNEL_WAKE_INJECT=1` | 10528 ns/op | 10998 ns/op |

Tiny TCP server without surgekv store/parser/manager:

| mode | op | clients | rps | p95 us | p99 us |
| --- | --- | ---: | ---: | ---: | ---: |
| `SURGE_THREADS=1` | ping | 32 | 30236 | 2219 | 2976 |
| `SURGE_THREADS=8` | ping | 32 | 10341 | 12715 | 14332 |
| `SURGE_THREADS=1` | get | 32 | 28914 | 2427 | 3165 |
| `SURGE_THREADS=8` | get | 32 | 8181 | 15575 | 18050 |
| `SURGE_THREADS=1` | get_pipe | 32 | 103992 | 291 | 301 |
| `SURGE_THREADS=8` | get_pipe | 32 | 107625 | 287 | 288 |

Trace snapshot from the bad case:

- `io_poll_calls=20996`
- `io_poll_allocs=41992`
- `io_poll_rebuilds=20996`
- `io_direct_waits=10788`
- `io_waiter_scan_entries=292357`
- `io_poll_wake_fd=10860`

### Root native channel benchmark

| mode | `channel_reused_reply` | `channel_new_reply` |
| --- | ---: | ---: |
| 1 thread | 3502 ns/op | 3965 ns/op |
| 8 threads | 9515 ns/op | 11104 ns/op |

This matches the surgekv probe: channel handoff becomes about 2.7x slower under
8 workers.

### Root native net benchmark

Selected rows:

| threads | mode | pattern | avg us/op | p95 us |
| ---: | --- | --- | ---: | ---: |
| 1 | echo | seq | 67.52 | 128.47 |
| 8 | echo | seq | 76.30 | 211.11 |
| 1 | direct | seq | 100.69 | 158.64 |
| 8 | direct | seq | 107.62 | 252.46 |
| 1 | manager | seq | 109.47 | 169.72 |
| 8 | manager | seq | 156.93 | 306.48 |

The root net fixture regresses less than surgekv because it is smaller and has
fewer live socket tasks, but it still shows the manager/channel cost.

## Initial runtime shape

Key files:

- `runtime/native/rt_async_internal.h`
- `runtime/native/rt_async_state.c`
- `runtime/native/rt_net.c`
- `runtime/native/rt_async_channel.c`

Current data model:

- `rt_executor` owns one global `waiter* waiters` list.
- Channel, join, timer, scope, blocking, and net waiters all share that list.
- `prepare_park()` appends a waiter.
- `pop_waiter()` scans and compacts the whole waiter list for one key.
- `has_net_waiters()` scans the same global list.
- `poll_net_waiters()` scans the global list, dedups fd entries, allocates
  `NetPollFd[]`, allocates `pollfd[]`, calls `poll()`, then completes waiters.
- Workers and the I/O thread can both enter net polling paths. The current guard
  is `worker_net_polling`, which prevents competing worker polls but does not
  make net polling globally single-owner.

## Working hypothesis

There are two runtime costs, and they should be fixed in this order:

1. Net scheduling churn for live non-pipelined TCP:
   repeated poll ownership handoff, wake-pipe churn, per-poll allocation, and
   global waiter scans.
2. Channel handoff cost under multiple workers:
   global executor lock plus global waiter scans plus cross-worker ready queue
   movement.

The first one explains the catastrophic surgekv tiny-TCP regression:
`SURGE_THREADS=8` is much worse than `SURGE_THREADS=1` even with no surgekv
store/parser/manager. The second one explains the remaining manager hop cost.

## Refactor plan

### Step 1: make net waiters counted

Change:

- Add `net_waiters_len` or equivalent to `rt_executor`.
- Maintain it in `add_waiter`, `remove_waiter`, `pop_waiter`, and any helper
  that drops stale waiters.
- Replace `has_net_waiters()` scans with a count check.

Expected effect:

- Removes repeated global waiter scans just to answer "is there net work?".
- Should reduce lock-held work in worker and I/O loops.

Validation:

- `make c-check`
- `go test ./internal/vm -run 'MT|Async|Net|LLVM'`
- `./scripts/bench_native_net.sh`
- `./scripts/bench_native_channels.sh`

### Step 2: make net poll globally single-owner

Change:

- Replace `worker_net_polling` with a runtime-wide net poll owner flag, or add a
  separate `net_polling` flag.
- Both worker-side fast poll and `rt_io_main()` must acquire that ownership
  before calling `poll_net_waiters()`.
- New net waiters should still call `rt_net_wake_poll()` so the active owner
  wakes and rebuilds its view.

Expected effect:

- Reduces duplicate polling and wake-pipe ping-pong.
- Target for surgekv tiny TCP bad case:
  `io_poll_calls` should move closer to `io_direct_waits`, instead of about 2x.
  Current: `20996` calls for `10788` direct waits.

Validation:

- Same as step 1.
- Compare `io_poll_calls`, `io_poll_wake_fd`, and p95/p99 in
  `surgekv/benchmarks/runtime-scheduling-probe.md`.

### Step 3: reuse net poll buffers

Change:

- Keep reusable `NetPollFd[]` and `struct pollfd[]` buffers on `rt_executor`.
- Grow them as needed; do not allocate/free them on every poll.
- Preserve the current `poll()` backend for now. Do not jump to epoll/kqueue in
  the first fix.

Expected effect:

- Current bad case has `io_poll_allocs=41992` for `io_poll_calls=20996`.
- Target after warmup: per-run `io_poll_allocs` should be near capacity growth,
  not `2 * io_poll_calls`.

Validation:

- `make c-check`
- `make cppcheck`
- native net/channel benchmarks
- surgekv runtime probe

### Step 4: complete ready net waiters with one pass per key

Change:

- Replace `complete_net_waiters()` looping over `pop_waiter()` with a helper
  that scans/compacts once for a key and wakes every matching task.
- Reuse the helper for read, accept, and write completion.

Expected effect:

- Reduces second-order waiter scans after `poll()` returns.
- This is smaller than a full waiter-index rewrite and keeps current FIFO
  semantics.

Validation:

- Same as step 3.
- Watch `io_waiter_complete_calls`, `io_waiter_completed`, and total scan
  counters.

### Step 5: only then consider channel waiter queues

Do not start here.

If steps 1-4 fix live TCP but channel benchmarks still show about 3x slowdown,
then split channel waiter lookup from the global waiter list.

Candidate change:

- Add keyed waiter buckets, or channel-local send/recv waiter queues.
- Keep task `wait_keys` for cancellation/select cleanup.
- Preserve FIFO per key.

Expected effect:

- Target `channel_reused_reply` at 8 workers: move from about 9500-10500 ns/op
  toward the 1-worker 3500-3800 ns/op baseline.

Reason to delay:

- This is more invasive than net poll ownership/buffer reuse.
- The current surgekv live-TCP collapse is already reproduced without the
  channel manager.

## First patch findings

Implemented and kept:

- `net_waiters_len` on `rt_executor`, maintained when waiters are added or
  removed.
- Runtime-wide `net_polling` ownership for net polls.
- Reusable `NetPollFd[]` and `pollfd[]` scratch buffers.
- One-pass completion for ready net waiters.

These changes worked as counter cleanup, but not as the main throughput fix.
The bad surgekv 8-thread run moved `io_poll_allocs` from `41992` to `2`, yet
`SURGE_THREADS=8 ping 32` stayed around `10k rps` and `get 32` stayed around
`8k rps`. The first patched trace still showed heavy scheduling churn:
`io_poll_calls=25194`, `io_direct_waits=13170`, `io_poll_wake_fd=12595`, and
`io_waiter_scan_entries=317256`.

## False and secondary findings

- Primary bottleneck is not per-poll allocation. Allocation was real waste:
  `io_poll_allocs` dropped from `41992` to near scratch-buffer growth only. The
  live TCP throughput did not move enough, so allocation is secondary.
- Primary bottleneck is not just `has_net_waiters()` scanning the global waiter
  list. The counter shortcut removed that scan path, but the bad 8-thread TCP
  shape remained.
- Worker-side net polling is not the collapse by itself. Removing worker-side
  net polls and routing net readiness through the I/O thread gave
  `ping 32 = 10232 rps` and `get 32 = 7951 rps`, essentially the same bad class
  as the baseline. This experiment should not be repeated as a fix.
- Removing wake-pipe notifications and relying on a `1ms` I/O poll tick is not
  an acceptable fix. It improved 32-client rows (`ping 32 = 15630 rps`,
  `get 32 = 15335 rps`) and reduced wake-pipe churn to
  `io_poll_wake_fd=0`, but it destroyed single-client latency under
  `SURGE_THREADS=8`: `ping 1 = 911 rps`, `get 1 = 842 rps`, with about
  `1.1-1.4 ms` latency. This proves wake churn matters, but no-wake polling is
  the wrong mechanism.
- Poll-set-aware wake coalescing is not enough. A patch that skipped wake-pipe
  writes when the active poll set already covered the same fd/interest kept
  single-client latency acceptable, but it did not move the bad rows:
  `ping 32 = 10412 rps`, `get 32 = 8445 rps`, with
  `io_poll_wake_fd=12724` and `io_waiter_scan_entries=330147`. This means the
  active poll set often does not already cover the next parked waiter; the
  runtime is rebuilding around very small net waiter sets instead of keeping a
  useful connection-level registry.
- A net-only fd/interest registry is not the primary bottleneck either. It cut
  `io_waiter_scan_entries` from about `330k` to `38610` and
  `io_poll_dedup_checks` to `0`, but the 8-thread live TCP rows stayed bad:
  `ping 32 = 9741 rps`, `get 32 = 8276 rps`. This proves the mixed waiter-list
  scan is waste, not the throughput wall.
- Front-queuing net-ready tasks is not enough. It made some p95 samples look
  better in the 32-client rows, but throughput stayed bad or worsened:
  `ping 32 = 9750 rps`, `get 32 = 7780 rps`, and p99 remained high. Queue
  priority alone is not the missing mechanism.
- Last-worker affinity for net-ready tasks is not enough. A patch that tracked
  the last worker per task and pushed net wakeups back to that worker's local
  queue kept the same bad throughput class: `ping 32 = 10036 rps`,
  `get 32 = 8292 rps`. `SCHED_TRACE` also stayed close to the current shape
  (`local=14184`, `inject=13248`, `steal=444`), so simple affinity does not
  remove the handoff churn.
- Worker-only net polling is worse. Disabling I/O-thread net polls pushed almost
  everything through worker local queues (`local=23922`, `inject=1`), but
  throughput dropped (`ping 32 = 8858 rps`, `get 32 = 7761 rps`) and worker
  sleep/wake churn jumped to about `23.6k`. This disproves "global inject is the
  main cause" as a standalone hypothesis.
- The current collapse is not explained by surgekv string/bytes parsing alone.
  The tiny TCP probe reproduces the bad `SURGE_THREADS=8` behavior without the
  surgekv store, parser, and manager. String/bytes work can still matter for the
  full Redis/Valkey gap, but it is not the first scheduling bottleneck shown by
  this probe.

## Confirmed finding

Bounded I/O-thread draining after net readiness moves the bad rows materially.
After `poll_net_waiters()` wakes tasks, the I/O thread drains up to 8 ready
continuations from the inject queue without waiting on the generic scheduler.

Latest probe:

- `SURGE_THREADS=8 ping 32 = 17801 rps`, p95 `6163 us`, p99 `7719 us`;
- `SURGE_THREADS=8 get 32 = 16448 rps`, p95 `6793 us`, p99 `8597 us`;
- `io_poll_calls=17850`, `io_poll_wake_fd=9347`,
  `io_waiter_scan_entries=146950`.

This is still short of the success target, but it is the first patch that moves
throughput and tail latency together.

## Next runtime hypothesis

The next fix should refine batching without letting the I/O thread become a
second full worker. Allocation cleanup, wake coalescing, net-only registry,
queue priority, simple worker affinity, and worker-only polling failed; bounded
post-poll draining succeeded.

Candidate designs:

- Tune the drain boundary with the smallest surface: drain only inject tasks,
  keep the limit fixed and low, and do not steal from worker local queues.
- Measure whether a smaller or larger fixed limit improves 1-client rows without
  losing the 32-client win.
- If the fixed limit is fragile, add one internal constant or env knob for
  runtime benchmarking only; do not expose public API yet.
- Keep channel-local waiter queues separate; the current tiny TCP collapse still
  reproduces without the surgekv manager hop.

Expected shape:

- Preserve the good single-client rows from the baseline.
- Reduce global inject/steal churn in 8-thread 32-client runs without forcing
  fixed `1ms` latency.
- Move `ping/get 32` toward the 1-thread rows before touching channel-local
  waiter queues.

## Current branch status

The current code keeps the small runtime cleanup patch plus bounded I/O-thread
draining after net readiness:

- counted net waiters;
- runtime-wide net poll ownership;
- reusable poll buffers;
- one-pass net waiter completion.
- drain up to 8 ready inject tasks after an I/O-thread net poll wakes waiters.

The larger failed experiments were reverted from code and kept only as notes in
this document. The latest probe for the current branch:

- `SURGE_THREADS=8 ping 32 = 17801 rps`, p95 `6163 us`, p99 `7719 us`;
- `SURGE_THREADS=8 get 32 = 16448 rps`, p95 `6793 us`, p99 `8597 us`;
- `io_poll_allocs=4`, `io_poll_calls=17850`,
  `io_poll_wake_fd=9347`, `io_waiter_scan_entries=146950`.

## Success criteria

Primary success:

- Tiny TCP `SURGE_THREADS=8` should no longer be slower than
  `SURGE_THREADS=1` for 32-client non-pipelined traffic.
- Practical first target:
  - `ping 32 clients`: from 10341 rps to at least 25000 rps.
  - `get 32 clients`: from 8181 rps to at least 24000 rps.
  - p95 under 4000 us for both rows.

Trace targets:

- `io_poll_allocs` no longer scales as `2 * io_poll_calls`.
- `io_poll_calls / io_direct_waits` approaches 1.0-1.2 in the surgekv tiny TCP
  8-thread run.
- `io_waiter_scan_entries` drops materially from the current 292357 bad-case
  baseline.

Non-goals for the first patch:

- No epoll/kqueue rewrite.
- No surgekv parser/store changes.
- No channel-local queue rewrite unless net-only changes fail to move the live
  TCP numbers.
