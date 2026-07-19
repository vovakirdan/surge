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
- Row 4 DONE (same test): the anchored rows. (a) Stale anchor: a
  corrupted-generation copy of a minted anchor answers STALE_TOKEN at
  dispatch entry (`rt_far_channel_pin` fails before any body exists),
  the pending is the sole owner and drops the shipped state exactly
  once, no body runs, and the failed pin attempt never touches the
  entry's in-flight count (proved by releasing the original,
  still-active lease and observing the registry entry reclaim
  immediately). (b) Happy path: the execute completes, the body runs
  without touching the channel, drop count stays 0, and releasing the
  driver's own lease afterward reclaims the entry at once — proving
  the dispatch-time pin was already released at the reply edge
  (`rt_remote_task_reply_owner_done` unpins before answering OK).
  (c) Cancel-bound: caller cancelled at
  `SP_IMMEDIATE_ON_BEFORE_PUBLISH` (anchor pinned, body created,
  unpublished) — the body still runs, the reply edge resolves with no
  caller to wake, the handed-off state never drops, and the pin
  releases exactly once at that same reply edge. No direct pin
  counter exists on the entry, so pin balance is proved through the
  far-channel registry's own reclaim rule instead
  (`rt_far_channel.c`: an entry reclaims only once it has both zero
  active leases and zero in-flight pins) — the driver mints the
  anchor, lets the scenario run, then releases its own lease last;
  a leaked dispatch-side pin would leave the entry live after that
  release, and none of the three rows do.
  BUG FOUND AND FIXED IN-TASK: the caller-teardown sweep
  `rt_immediate_on_release_owned` filtered on
  `op == EXECUTE || op == CHANNEL_SELECT` and omitted
  `EXECUTE_ANCHORED`, unlike every sibling site that branches on this
  op triple (`rt_remote_task_dispatch.c` ~160,
  `rt_remote_task_completion.c` ~48, `rt_remote_task_deadlock.c`
  ~146) — so a cancelled caller with an UNBOUND anchored execute
  never resolved the pending, the late dispatch created the body
  anyway, and the family contract ("unbound abandoned request must be
  refused, no body") was silently violated for exactly the anchored
  variant. One-line fix: the anchored op joins the sweep filter (the
  anchored request path already sets `caller_task_id`). Pinned by the
  `anchored-cancel-unbound-refuses-body` row (drop x1, no body, no
  pin — refusal happens at the snapshot check BEFORE the pin);
  negative control verified — the row fails on the pre-fix runtime.

## Status

IN PROGRESS (2026-07-19). Rows 1-4 landed (incl. the anchored sweep
fix). Row 5 (gates) pending.
