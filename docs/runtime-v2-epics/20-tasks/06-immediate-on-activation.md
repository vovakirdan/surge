# Epic 20 Task 6: Immediate-On Activation

Placement `on` and anchored `on ch` ride the Task 5 ownership
contract (publication-accepted handoff, documented on the pending's
drop fields): this task proves the immediate-on family's edges with
the same discipline — every row publishes a DROPPABLE state and the
harness drop stub is the exactly-once census. The family differs
from spawn-on in one recorded way: there is no publicly observable
far Task handle, so caller teardown works through the
`rt_immediate_on_release_owned` sweep (`caller_task_id`), and an
UNBOUND request abandoned by its caller must be REFUSED by a late
dispatch (no body), while spawn-on's abandoned in-flight request
still runs its body.

## Rows (order = execution order)

1. **Refusal edges (placement `on`).** Queue-full and
   destination-shutdown at request publish: the pre-link/pending
   path is the sole owner — state drops exactly once, no body, the
   caller resumes with the refusal status.
2. **Caller-teardown split.** (a) Caller cancelled while the request
   is UNBOUND (held at dispatch entry): the teardown sweep resolves
   the pending, the late dispatch refuses to create a body, state
   drops exactly once. (b) Caller cancelled while the request is
   BOUND (held at `SP_IMMEDIATE_ON_BEFORE_PUBLISH` or later): routed
   cancel; the reply edge resolves exactly once; the handed-off
   state reclaims compiled-side (drop count stays 0).
3. **Stale/duplicate redelivery.** Duplicate EXECUTE request after
   resolution and stale reply after resolution: each releases only
   its own message reference (extra-pending-reference technique from
   the Task 5 rows; refcount fall = dispatch-completion signal).
4. **Anchored rows (separate, per the epic).** (a) Anchor stale at
   dispatch: a stale-generation channel anchor refuses the execute
   and leaks no pin. (b) Pin/unpin balance: dispatch-time pin is
   released exactly once on every exit (reply, refusal, caller
   cancel). (c) Reply cancellation: a caller gone mid-anchored
   execute unpins exactly once and the reply resolves exactly once.
5. **Gates.** New rows wired into the remote-task acceptance line;
   `make check`; sync-point allowlist updated if row 2a needs an
   immediate-on dispatch-entry window; scoped Sentrux note.

## Progress

- Row 1 DONE (`TestRuntimeV2ImmediateOnAbandonEdges`, transport gate):
  refusal x2 (queue-full via data-lane fill, destination shutdown) —
  the sole-owner pending drops the droppable state exactly once, no
  body, caller resumes with the refusal status.
- Row 2 DONE (same test): the caller-teardown split. UNBOUND —
  caller cancelled while the execute request is held at the NEW
  `SP_IMMEDIATE_ON_BEFORE_DISPATCH` window: the teardown sweep
  resolves the pending (REFUSED), the late dispatch steps aside at
  its snapshot check, no body, state drops exactly once (the
  bound/unbound fork keys off `handle.task_id`, still 0 before the
  bind). BOUND — caller cancelled at
  `SP_IMMEDIATE_ON_BEFORE_PUBLISH` (body created + owner-registered,
  unpublished): exactly one routed cancel (`cancel_routed`), the
  body still runs (the harness body has no suspension points — both
  completions are legal per the cancel-route contract), the reply
  edge resolves with no caller to wake, and the handed-off state
  never drops through the pending.
- Row 3 DONE (same test): redelivery after resolution. A duplicate of
  the ORIGINAL execute request carries the request-scoped token while
  the pending's handle was rebound to the body task's generation at
  the bind — the token match fails, exactly one stale drop is counted
  (`remote_task_stale_drops`, the dispatch-hit assertion), the
  stale-token answer flows into the already-resolved (hence no-op)
  reply edge, and only the message reference is released. A
  redelivered REPLY matches the resolved pending: finish no-ops,
  reference released. Completion signal = the pending's refcount
  falling back to the driver's own reference (same technique as the
  spawn-on rows); the immediate pending got its own release/acquire
  twin pointer.
- Rows 4-5 pending (anchored stale/pin-unpin/reply cancellation;
  gates).

## Status

IN PROGRESS (2026-07-19). Rows 1-3 landed.
