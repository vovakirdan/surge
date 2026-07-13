# Epic 17 Task 5: Stabilization Seam + Bench + Closeout

## The selector lifecycle seam (what B and A consume later)

The reusable selector lifecycle the epic doc's lift path names is
already factored as callable surfaces, not inline logic; this section
IS the seam record.

| Lifecycle piece | Owning surface | What B/A would reuse |
| --- | --- | --- |
| Winner arbitration | `rt_select_poll` (`rt_async_select.c`) | unchanged — any model arbitrates through the local scan on some shard |
| Terminal transition + loser cleanup | `mark_done` (`clear_wait_keys`/`clear_select_timers`) | unchanged — completion of the deciding task IS the cleanup |
| Cancellation | `dispatch_execute_cancel` gate (op-tagged pendings) + `rt_immediate_on_release_owned` sweep | B adds its sub-pending ops to BOTH gates (the two Task-2 defects show what forgetting one costs) |
| Stale-wake suppression | `clear_wait_keys` re-arm at `rt_select_poll` entry + park-generation validation | unchanged |
| Orphaned-reply consumption | `dispatch_reply` consume discipline | unchanged — B's N sub-replies each ride it |
| Detector representation | `find_channel_parked_body` arm-count branch | B must collapse its N sub-pendings into ONE logical suspect the same way |
| Arm-table transport | `rt_far_channel_select_arm` + pin-all-or-nothing dispatch loop | B splits the table per owner; the per-arm pin/unpin loop is reusable as is |

Rule for the tail work (recorded as debt): B and A change HOW arms
wait, never WHAT the lifecycle pieces above promise; any tail vertical
starts by re-reading this table and the Task 2 defect record.

## Bench (2026-07-13, committed tree, LLVM)

`scripts/bench_crossing.py --iterations 2000`, new `select-ready`
probe (one anchored feed + one remote select on the ready arm = two
round trips per iteration, matching `on-ch-pair`'s shape):

| probe | shards 1 | shards 2 | shards 8 |
| --- | --- | --- | --- |
| on-ch-pair (us/rt) | 14.6 | 126.8 | 125.4 |
| select-ready (us/rt) | 15.1 | 126.3 | 125.5 |

The proxy selector costs exactly one anchored round trip — the C-model
cost prediction (one remote transaction per select regardless of arm
count) confirmed empirically. Steady-state anchored costs unchanged
within noise vs the Epic 16 baseline (on-ch-pair 126.8 vs 126.3 us at
2 shards across the epics).

## Debt disposition

- NEW RV2-DEBT-033: the multi-owner select tail — Model B (arms as
  anchored micro-ops) as the honest slow path, then A only behind a
  profile signal AND a waiter-store hardening vertical; plus the
  deferred surface pieces (mixing far arms with local/task/timeout/
  default arms; today SEM3176 splits them). Profile-gated: no vertical
  starts until a workload shows the single-owner restriction binding.
- RV2-DEBT-030/031/032 untouched by this epic (positions recorded in
  the task docs).

## Closeout gates

make check, transport umbrella (exit captured directly), remote-task
behavior suite + deadlock rows, ready-requeue proof pair, crossing e2e
gate (genesis/on-ch/share/select at SHARDS=1/2/8) — all green on the
committed tree; second full pass at the closeout commit. Sentrux four
scopes at closeout: see the epic doc closeout section.
