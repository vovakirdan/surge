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

## Status

IN PROGRESS (2026-07-17).
