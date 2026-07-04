# Epic 7 Task 12: Lock-Split Trace Counters And Benchmarks

**Kind:** runtime code + benchmark evidence. **Depends on:** Task 11.

**Goal:** make the split observable (control-lane pressure, cross-shard
wake volume, absorbed spurious wakes, collect-then-wake batching, owner
re-placements) and produce the net + channel benchmark evidence against
the Task 1 / Epic 6 closeout baselines.

## Scope

- Five `SURGE_TRACE_EXEC=1` counters on the `TRACE_EXEC` line:
  `control_lock_acquired` (in `rt_control_lock`), `cross_shard_wakes`
  (wake leaf, waker's shard != owner shard), `spurious_wakes_absorbed`
  (`park_requeue_locked`), `collect_wake_batches` (non-empty batches in
  `wake_key_all_with_policy` and the net completion collector),
  `owner_replacements` (`rt_task_replace_owner`).
- `scripts/bench_native_net.sh` reports the five counters as new Runtime
  Trace columns.
- Final reports: `build/benchmarks/rv2-e7-task12-final-epic6-matrix.md`
  (shards 1 and 8 × connections 1/8/32/1024, direct/seq, 8 req/conn) and
  `build/benchmarks/rv2-e7-task12-final-channels.md`
  (`bench_native_channels.sh`, modes 1/2/4/8/default).

## Results (vs baselines; full tables in the evidence ledger)

- Net small-load rows (1-shard 1/8/32 conns) within noise of the Task 1
  baseline; 1-shard 1024 conns 1.531s vs 1.537s baseline.
- 8-shard 1024 conns: 1.94s vs 2.52s baseline (**-23%**), but still 1.27×
  the 1-shard row. Counters name the next serialization point:
  `control_lock_acquired=215781` for 8192 requests (~26 per request) —
  task lifecycle (create/join/done) still runs on the control lane;
  recorded as the Epic 8 candidate in the closeout.
- Channels (ns/op, pre-B2 baseline → post-split): ping_pong@2
  21129→15235 (-28%), reused_reply@2 -18%, new_reply@2 -22%,
  ping_pong@default(32) -24%; mode-1 probes -3..-11% (reused_reply@1
  +2%, noise). `channel_sync_new_reply` 42112→47791 (+13%) at 2 workers
  and 192162→449293 (+134%) at default/32: the blocking-compat sync
  lane pays for the closed lost-ack/misdelivery races (owner-locked
  consume-or-arm plus the 10ms slice worst case; see Task 11 B2);
  accepted and recorded as `RV2-DEBT-017`.

## Checks

c-check/cppcheck; counter smoke via `TRACE_EXEC` on the compat repro;
`runtime-v2-lock-check` green; `make check` pre-commit; benchmarks built
from the current checkout (script embedded-commit check).

## Success Criteria

Counters visible in `TRACE_EXEC` and the net bench report; final matrix +
channels reports recorded with per-row explanations; own commit.
