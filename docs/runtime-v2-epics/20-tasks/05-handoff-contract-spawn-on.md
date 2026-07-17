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
  REMAINING for row 4: ACK-enqueue-failure after publication
  (needs deterministic control-lane saturation at ack time);
  cancel-after-publication-before-first-poll (needs a first-poll
  window); e2e-level caller-cancel integration through the lease
  route (rt_far_task_lease_release_route) rather than direct
  abandon.
- Rows 5-7 pending.

## Status

IN PROGRESS (2026-07-17). Row 1 code landed; behavior row + rows 2-7
remain.
