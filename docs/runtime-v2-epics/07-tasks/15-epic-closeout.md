# Epic 7 Task 15: Epic Closeout

**Kind:** closeout. **Depends on:** all.

**Goal:** consolidate evidence, update durable docs, close or record debt,
and state the Epic 8 handoff plus the syntax gate.

## What Closed

- Tasks 1-14 complete; every Task 5 static gate green; the 9 cross-shard
  behavior modes green at `SURGE_SHARDS=1` and `3`;
  `make runtime-v2-lock-check` promoted into `runtime-v2-check` and CI.
- Evidence ledger `07-evidence.md` holds per-task entries including the
  Task 11 peel slices (B1a/B1b/B2/B4) and the Task 12 benchmark matrix
  with the new lock-split counters.
- Benchmarks: 8-shard/1024 net row -23% vs the Epic 6 closeout baseline;
  small-load rows within noise; multi-worker async channel probes
  -14..-25%; `sync_new_reply@2` +42% recorded as `RV2-DEBT-017`.

## Debt Recorded Or Updated

- `RV2-DEBT-002` updated: sync-helper hang root causes fixed in B2
  (lost-ack overwrite + stale-entry misdelivery); residual is a
  load-flake plus the bounded 10ms compat slice.
- `RV2-DEBT-004` closed (`rt_net.c` off the allowlist);
  `RV2-DEBT-003`/`005` updated with the new ceilings.
- New: `RV2-DEBT-015` (adopted 8x1024x100 starvation, present at
  baseline, diagnosis + `stallrepro.py` evidence), `RV2-DEBT-016`
  (control lane now dominated by task lifecycle, ~26 acquisitions per
  request — the Epic 8 candidate), `RV2-DEBT-017` (sync-channel compat
  regression), `RV2-DEBT-018` (rare ~2.3ms exit=1 empty-output harness
  transient, tied to `RV2-DEBT-011`).

## Epic 8 Handoff

The lock split leaves exactly one steady-path control-lane consumer of
consequence: task lifecycle. `__task_create`, join poll/await, `mark_done`
(control conditions), scope registration, and handle release all still
take `ex->lock`, measured at ~26 control acquisitions per request on the
8-shard/1024 matrix row (`RV2-DEBT-016`). Epic 8 should move task
create/join/done onto shard lanes (the task table is already an atomic
snapshot structure; owner stability rules D3/D5 already hold), after
which the 8-shard row must at least match 1-shard. Secondary candidates:
the select slow lane (still control-serialized by design) and the
compat channel lane retirement (`RV2-DEBT-017`).

## Syntax Gate

No Surge-language syntax changed in this epic; the runtime ABI
(`rt_channel_*`, task builtins) is unchanged. Guest-visible behavior
deltas: none intended; the B2 protocol fixes previously corrupting
sync-channel races.
