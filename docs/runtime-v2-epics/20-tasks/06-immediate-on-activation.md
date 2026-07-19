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

## Status

IN PROGRESS (2026-07-19).
