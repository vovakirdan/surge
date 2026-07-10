# Epic 14 Tasks 2-3: Anchored-Op Behavior Rows And Runtime

**Status:** in progress (2026-07-10).
**Kind:** test-first harness rows + the anchored execute runtime.
**Depends on:** Task 1 (contract), Task 1.5 (registry, tokens, mint).

## Design (pinned before rows)

The anchored block runs as an ordinary body task on the anchor's owner
shard, so most contract obligations are INHERITED from local machinery
rather than implemented:

- `ch.send/recv/close` inside the body resolve the local channel via
  `rt_far_channel_resolve` and enter the owner's local channel lanes — the
  linearization point promised by the ordering contract (decision 2) and
  the owner-side close-wake obligation (decision 4a) are the local
  machinery's existing behavior.
- caller cancel, orphaned-reply consumption, teardown termination of
  in-flight requests: the execute/reply discipline shipped in the placement
  vertical, unchanged.

New runtime surface (this task):

- `rt_immediate_on_execute_anchored(anchor, poll_fn, state, pending, kind,
  bits)` — kind-checked, destination = the anchor's owner shard; the
  dispatch side validates the anchor against the channel registry BEFORE
  creating a body (a stale anchor answers the stale status without running
  anything — kinder and cheaper than a body that fails on resolve) and pins
  `entry->inflight` for the block's duration (release/teardown wait on the
  pin per the registry contract; the pin drops when the reply edge
  resolves).
- the self-deadlock detection (decision 5): the all-shards-quiescent check
  at the worker idle-park boundary with the actionable panic
  (`rt_remote_task_deadlock.c`; double-checked scan, on in every build,
  `SURGE_REMOTE_DEADLOCK_DETECT=0` opts out for embedders whose external
  threads feed channels through FFI — the quiescence model cannot see
  non-runtime threads).

## Row Plan (the epic's race/failure matrix, harness level)

1. success round trip: anchored body sends/recvs/closes on a minted
   channel from a non-owner shard; values observable owner-side.
2. full-channel send: body suspends on the owner's local waiter; the
   owner's dispatcher stays live (a second unrelated message is served
   while the body is parked); reply arrives after capacity frees.
3. stale anchor: released entry -> STALE without body creation (owner-side
   counter proves no task was spawned); wrong-kind anchor (task token) ->
   invalid.
4. close-vs-send: close wakes the parked body with the closed outcome
   through the single reply; never success-after-close.
5. caller cancel vs completion: exactly-one reply-edge consumption; a late
   channel wake cannot resurrect the cancelled body (stale-waiter row).
6. owner teardown mid-flight: deterministic stale-owner error to the
   suspended caller.
7. self-deadlock reproducer: full channel whose only consumer is the
   initiating caller -> the decision-5 panic naming the channel and the
   parked operation. DONE (`anchored-self-deadlock`, expected-panic rows on
   1 shard/2 workers and 2 shards; the single-worker configuration starts
   no worker threads, so its quiescence stays with the driver-side "async
   deadlock" panic).
8. no-counterparty mint (genesis handoff): mint with no owner-side
   consumer + capacity-blocked sends -> same decision-5 outcome. DONE
   (folded into row 7: the reproducer mints and blocks on capacity with no
   consumer anywhere).
9. inflight-pin vs release: release during an active block waits for (or
   deterministically rejects per contract) the pinned entry; no
   use-after-free of the channel under the body's feet. DONE
   (`anchored-pin-vs-release`: release returns OK and flips the entry to
   RELEASING, the token stops resolving, the body completes on its
   already-resolved pointer, reclamation waits for the reply-edge unpin).
10. leak audit: pendings, tokens, registry entries, body tasks after each
    row.

Sentrux committed-tree baselines at this task's start: root `6185`,
`internal` `6515`, `runtime` `5315`, `runtime/native` `5408`.

## Stop Conditions

Inherited from the epic (dispatcher never blocks; quiescence check must
separate deadlock from legitimate idle without cross-shard walking, else
detection narrows to debug builds and the release behavior goes to design
review; anchored ops never walk owner state from the caller side).
