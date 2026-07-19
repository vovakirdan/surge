# Epic 20 Task 5: Handoff Contract + Spawn-On Activation

The pivot task of the epic: state the ownership contract for a
shipped crossing state ("pending owns → body owns", linearized at a
NAMED point), fix the two ledgered bugs that sit on that contract,
and prove the spawn-on abandon edges with dispatch-hit + census rows.

## Rows (order = execution order)

1. **RV2-DEBT-047 — shutdown drain repair.** The drain's kind
   allowlist predates Epics 13-17: queued `IMMEDIATE_ON_*` /
   `FAR_CHANNEL_*` / `CREDIT_CONTROL` messages panic before the
   release helpers run, though `rt_remote_task_release_msg_payload`
   already releases those kinds. Replace the allowlist with
   kind-complete release dispatch; deterministic
   queued-message-at-shutdown row per family.
2. **RV2-DEBT-051 — heap-bearing captures.** Root-cause the
   pack/unpack mismatch (body-side `j.id` reads a pointer bit
   pattern when the struct carries a heap field; all-Copy captures
   masked it) and the happy-path leak (48/42 over the scenario
   window). Fix; goldens + e2e over a mixed Copy/heap capture; the
   Task 4 census row lands here (balanced window, execution witness,
   sync-main entry shape).
3. **Named linearization point.** Document (in-code, behavior-named)
   the exact transition where pending stops owning the state and the
   body starts: publication accepted = the body owns; every earlier
   exit = pending drops via `__surge_drop_call`; every later exit =
   compiled-side reclamation (fork 5 model). No path may see both.
4. **Spawn-on abandon edges** (each: deterministic edge-forcing row
   with a dispatch-hit/count assertion AND a census row): cancel
   before dispatch; cancel after task creation, before publication;
   cancel after publication, before first poll (compiled-side);
   refusal (queue full / destination shutdown); ACK/reply enqueue
   failure after publication; forced races around publication/handoff
   and first poll (sync-point harness).
5. **Three stale-generation rows:** stale request before body
   creation (pending sole owner, drops once); stale/duplicate message
   after handoff (must NOT drop body-owned state); stale ACK/reply
   after resolution (releases only its message reference).
6. **Owned results (reply edge):** a completed body's owned result is
   reclaimed exactly once when the awaiting caller is gone
   (cancelled/released before consuming the reply).
7. Gates: `make check`, transport suites, Sentrux comparison; new
   tagged tests wired per gatecheck.

## Progress

- Row 1 (RV2-DEBT-047) CLOSED: kind-complete release switch (commit
  `9f27d86d`) + the `shutdown-queued-kinds` harness row (commit
  `5de8d250`) — every post-Epic-13 kind enqueued, drained without
  panic, both lanes empty; static guards pin the release switch and
  the fail-closed unknown-kind message.
- Row 2 (RV2-DEBT-051) CLOSED — see the ledger row for the full
  record. Two fixes: (a) sema — `own` now consumes its temp
  candidate (`ExprUnaryOwn` + `consumeTempCandidate`), killing the
  caller-side statement-end drop of the very box the own binding
  aliases (both backends lower `own` as identity; all-Copy structs
  were masked because they are not droppable). The suspected
  pack/unpack arity mismatch was a misread — `own T` layout resolves
  to T's layout, so the 16B state box has one slack slot, but both
  sides agree on one pointer field at offset 0. (b) MIR — crossing
  poll bodies (`rewriteSpawnOnPollReturns`) release the state
  envelope box via `__async_state_free` before AsyncReturn, matching
  the local-async resume-frame model; the pending-side
  `__surge_drop_call` covers only never-handed-off states, so this
  IS the row-3 linearization in code. Proof: arrival e2e
  (`TestRuntimeV2CrossingHeapCaptureArrivesIntact`, content-checked
  mixed capture, shards 1/2/8, transport gate) + census e2e
  (`TestRuntimeV2CrossingHeapCaptureCensusBalanced`, per-iteration
  far growth == local spawn/await growth, 1 shard, drop gate).
  Found + split out: RV2-DEBT-052 (compare over an owned union leaks
  the scrutinee box, local and far alike; the census row cancels it
  by comparing far vs local). The Task 4 census row is satisfied by
  the census e2e above.
- Row 3 DONE — the linearization point is NAMED in code: the
  PUBLICATION-ACCEPTED HANDOFF, the dispatch-lane `state_owned = 0`
  store immediately after `rt_remote_spawn_publish_body_task`
  succeeds. Contract documented once on the spawn pending's drop
  fields (`rt_remote_spawn_internal.h`, twin pointer in
  `rt_remote_task_internal.h`), with the no-double-release argument
  (the dispatch lane still holds a pending reference when it clears
  the flag, so the plain store orders before the final acq_rel
  refcount drop). All three dispatch sites carry the name (anchored
  hands off through the shared immediate-on dispatch).
  `TestRuntimeV2RemoteStateHandoffStaticContract` pins: 4 publish
  sites record the obligation, exactly one handoff per dispatch
  family AFTER the accepted publication, no anchored second handoff,
  and both final-release drop sites stay gated on `state_owned`.
- Row 4 PARTIAL (`TestRuntimeV2RemoteSpawnAbandonEdges`, transport
  gate): all rows publish a DROPPABLE state (harness drop stub = the
  exactly-once census). Landed: refusal x2 (queue-full and
  destination-shutdown both drop exactly once, no body); abandon x3
  at armed windows — two NEW spawn-on sync points
  (`SP_REMOTE_SPAWN_BEFORE_DISPATCH` at dispatch entry,
  `SP_REMOTE_SPAWN_BEFORE_BODY_PUBLISH` between body creation and
  publication, twin of the immediate-on window) plus the existing
  `SP_REMOTE_SPAWN_BEFORE_ACK`. Semantics pinned: an abandon while
  the request is in flight still runs the body (the state hands off;
  drop count stays 0) and the ack resolves as an owner-routed
  release. Harness gotcha recorded in-code: after abandoning, the
  caller task must never touch the pending again (finish under
  abandoned releases the caller's reference).
  Row 4 addendum (ack edge): the planned ACK-enqueue-FAILURE row
  turned out to be pinning the wrong thing — `try_drain_one` pops
  CONTROL-FIRST, so the rescue drain in
  `rt_remote_spawn_enqueue_with_drain` always frees control room and
  a saturated lane CANNOT fail the ack (the failure branch is
  reachable only through transport shutdown; its handle-release
  ordering stays pinned by the FailurePathStaticGuards row). What
  the runtime actually guarantees is now the row:
  `ack-rescue-drain-after-handoff` — control lane saturated at the
  ack window, publication still resolves OK, body runs, handed-off
  state untouched. Harness gotcha: at 1 shard the MAIN thread drives
  execution inside rt_task_await, so the mid-window driver must be a
  helper pthread.
  REMAINING for row 4 (unchanged):
  cancel-after-publication-before-first-poll (needs a first-poll
  window); e2e-level caller-cancel integration through the lease
  route (rt_far_task_lease_release_route) rather than direct
  abandon.
- Row 5 DONE (`TestRuntimeV2RemoteSpawnStaleGenerationRows`,
  transport gate; droppable state, drop stub = census):
  stale-request-before-body (pending resolved via fail-all while the
  request is held at the dispatch-entry window → no body, sole-owner
  pending drops exactly once); duplicate-request-after-handoff and
  stale-ack-after-resolution (an extra pending reference taken in the
  ack window models the redelivered copy's payload reference; after
  OK the redelivered message is drained → releases only its own
  message reference, drop count stays 0, `child.ran` stays 1 — the
  child flag became a counter for exactly this assertion).
- Row 6 RESOLVED AS LEDGERED DEBT: the owned-results reply-edge
  investigation (codex deep-dive, claims verified by direct read)
  confirmed NO reclamation obligation exists for a heap-carried
  result when the caller abandons the reply — two silent-leak paths
  (release-while-DONE discards `task->result_bits` unseen; a landed
  reply strands in the orphaned caller pending, whose
  `caller_task_id` is a dead field for AWAIT/CANCEL). Opened
  RV2-DEBT-053 with the full trace and the preferred fix shape
  (populate `caller_task_id` + widen the existing
  `rt_immediate_on_release_owned` sweep, then thread a
  `result_drop_fn_id` like the state's). The fix is a design-slot
  item, not a row-sized change — out of this task per the
  no-fix-without-design rule.
- Row 7 pending (final gates + doc sync).

## Status

IN PROGRESS (2026-07-17). Row 1 code landed; behavior row + rows 2-7
remain.
