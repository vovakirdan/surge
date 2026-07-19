# Epic 20 Task 7: Remote Select Symmetry + Abandon Coverage

The remote half of the select send-arm ownership fork (resolved (b):
symmetry in this epic). Reconnaissance findings that scope the task:

- **Far channels are Copy-only by construction today**: the
  `channel_on` crossing gate requires a Copy payload type
  (`crossingRecordExecutable`, ChannelCreate case), so a non-copy
  owned payload CANNOT reach a far send arm from any program — the
  sema gap below is defense-in-depth, not a live double-free.
- **The sema gap is real**: `typeSelectAwaitExpr` gates the Task-3
  ownership discipline (`checkSelectSendPayloadOwnership`) behind
  `isChannelType`, which is false for far channels — a far send arm
  with an owned payload would compile untracked (the pre-Task-3
  class) the day non-copy far channels open.
- **The commit is already atomic and self-reporting**: the owner
  body delegates to the unchanged local `rt_select_poll` under ONE
  `rt_control_lock` critical section that covers both the
  cancellation check and the arm-scan delivery; caller-cancel also
  runs control-held. Winner index (kind=1, bits=K) therefore IS the
  authoritative commit bit — kind=2 means no arm was touched. No new
  protocol is needed; what is missing is caller-side CONSUMPTION of
  that bit for drop obligations, which only matters for non-copy
  payloads.
- **Cancelled frames reclaim at the deferred abandon point**, not
  eagerly (`rewriteAsyncReturns` keeps cancelled state alive) — any
  future loser-arm reclamation must ride that point.

## Scope split

IN THIS TASK (reachable and provable today):
1. Sema symmetry — far send arms get the Task-3 discipline.
2. Winner-side per-arm machinery for far selects where it rides the
   existing select surface for free (the remote select keeps its
   select surface; arm dispatch stays caller-side).
3. Deterministic runtime rows over Copy payloads: exactly-once
   delivery under cancel-vs-commit races; double-cancel idempotency;
   refusal regression guard.

DEFERRED TO VERTICAL 3 (observable only with non-copy far channels,
which require remote-free ownership): the abandon-time
reconciliation of the commit bit (gate the cancelled caller's
deferred drop on reply resolution — design (d2), recorded below so
the vertical picks it up whole), and the non-copy e2e rows (winner
not double-dropped, losers reclaimed once; abandoned caller skips
exactly the committed arm). These become live the day the
ChannelCreate gate opens to non-copy payloads, and MUST land in the
same change.

## Rows (order = execution order)

1. **Sema symmetry.** `checkSelectSendPayloadOwnership` fires for
   far channel send arms too (payload type via the far inner
   channel); SEM3140 own-marker and SEM3141 whole-binding goldens
   over a declared `far Channel<T>`; per-branch move/ArmDrops
   bookkeeping applies to far arms through the shared select
   surface (golden/MIR row pinning arm-drop placement for a far
   select with a non-copy payload type — compile-level only, the
   channel need not be constructible).
2. **Cancel-vs-commit race row** (sync-point): cancel lands after
   `rt_select_poll` chose a winner but before the reply is enqueued
   — the reply must stay kind=1 with the winner index honored
   (pins the one-lock atomicity claim under forced interleaving,
   not just by code reading). Requires a window between the owner
   body's winner commit and its async-return/reply.
3. **Cancel-before-dispatch row**: caller cancelled while the select
   request is unbound — no arm touched, refused pending, no body
   (the CHANNEL_SELECT op is already in the teardown sweep filter).
4. **Double-cancel idempotency row**: two independent cancel routes
   on one select pending — `cancel_routed` yields exactly one
   routed cancel and one reply resolution.
5. **Refusal regression guard**: owner-side refusal after arms
   shipped (stale lease mid-pin) — the pending's release frees the
   arm TABLE as inert bytes and never inspects `send_bits`; the
   caller's own frame remains the only owner of payload bindings.
6. Gates: rows wired into the transport gate; `make check`;
   sync-point allowlist for the new owner-side window.

## Status

IN PROGRESS (2026-07-19). Reconnaissance complete (runtime half by
codex, sema/gate half by lead); scope split recorded.
