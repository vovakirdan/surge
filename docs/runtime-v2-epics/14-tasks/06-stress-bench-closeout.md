# Epic 14 Task 6: Stress, Leak Census, Bench, Closeout

**Status:** complete (2026-07-12).
**Kind:** stress rows + bench row + debt + epic closeout.

## Rows

- `anchored-queue-full` (behavior harness, SHARDS=2): a saturated
  destination data lane answers QUEUE_FULL per attempt with the pending
  rolled back and nothing wedged; a block already parked on the owner
  completes through the control-lane reply while the data lane is still
  saturated; a post-drain attempt succeeds; `credit_stalls` and the
  fallback tripwire stay zero. (The saturation gate parks the in-flight
  body on channel capacity so the owner is genuinely idle — a busy owner
  drains the fill before the flooded enqueue can observe it.)
- `anchored-leak-audit` (row 10 of the race/failure matrix): 48
  mint/send/recv/release cycles alternating owner shards, every eighth
  releasing while a pinned block is active; afterwards the pending list
  and the channel registry census (`rt_far_channel_debug_live_count`)
  are empty and the fallback tripwire is zero.

- `anchored-cross-producer-order` (post-closeout addendum): the
  cross-producer negative observation from the acceptance draft —
  deterministic inversion via the gated body proves values follow the
  owner's local-lane execution order, not cross-producer block-start
  order. The closeout initially over-scoped this criterion as blocked on
  RV2-DEBT-025; only the source-level two-producer PROGRAM is (the
  criterion never required source level).

## Bench Row (same harness/host as the Epic 13 baseline, 2000 iters)

| probe | shards | rt/sec | us/rt |
| --- | --- | --- | --- |
| on-ch-pair | 1 | 72491 | 13.8 |
| on-ch-pair | 2 | 9166 | 109.1 |
| on-ch-pair | 8 | 8970 | 111.5 |

One iteration is TWO anchored blocks (send + recv), so the per-block cost
is ~6.9/54.6/55.8 us at 1/2/8 shards — within noise of the plain
immediate-on block (6.8/54.1/78.9): the registry pin + cached resolve add
no measurable overhead to the execute/reply spine. Baseline probes
reproduced within noise on the same run (spawn-await 94372/6078/6376,
immediate-on 146540/18490/12680).

## Debt

- RV2-DEBT-031: credit-based flow control deferred; `credit_stalls`
  instrumented and asserted structurally zero.

## Gates

Behavior suite (incl. both new rows) green twice; c-check / c-warnings /
cppcheck green; transport + crossing gates green; `make check` green.
Closeout Sentrux recorded in the epic closeout note.
