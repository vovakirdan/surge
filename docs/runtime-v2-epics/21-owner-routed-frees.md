# Epic 21: Owner-Routed Frees (vertical 3 of the reclamation arc)

**Status:** CORE VERTICAL COMPLETE; TASK 9 ACCEPTANCE CLOSEOUT OPEN
(tracked by RV2-DEBT-125). The owner-routed reclamation path shipped,
including RV2-DEBT-059's fix in `bdcc0695`;
Task 9's benchmark, named probe matrix, Phase-5 seam, and final debt
dispositions were not performed, and Task 4's RV2-DEBT-054/056 tails
remain open. RV2-DEBT-061 and RV2-DEBT-062 also remain separate
residuals, so this status must not be read as full charter closeout.
Design review concluded 2026-07-20 via a
tier-based sequencing decision (user-approved): Tier 0 first (057,
055 — census-poisoning and compile-breaking bugs, independent of
crossing), then Tier 1 in dependency order (060+048-residual →
053a ∥ the abandon-reconciliation design → 053b/059 → the non-copy
gate + d2, last). This ratifies forks 1 (Phase 5 stays a successor
epic) and 5 (the gate opens last, after the reconciliation design
and channel lifecycle land) explicitly; forks 2-4 and 6 stand at
their recorded leanings (no objection raised). Tier 2 (058, 054+056,
the 052 residual) and Tier 3 (allocator Phase 5, the 001-026 ledger
revision) proceed as scheduling allows without blocking Tier 0/1.
RV2-DEBT-049/050 (async-entrypoint harness reliability) stays a
separate discussion outside this epic, per the user's standing
preference recorded at the Epic 20 closeout. Charter source:
`20-tasks/08-bench-census-closeout.md` (the Epic 20 closeout duty).
Arc plan: `19-candidates.md` (local emission → crossing activation →
owner-routed frees). Carries RV2-DEBT-053, 059, 060, the Epic 20
Task 7 vertical-3 deferral (abandon-time commit-bit reconciliation +
non-copy far-channel e2e), the RV2-DEBT-048 channel-object residual,
and the 054-058 dispositions.

Draft v2 (2026-07-20) incorporates the codex second-opinion review
(extensive direct code reads; findings verified by the lead where
cited): RV2-DEBT-053 is TWO independent defects (owner-side and
caller-side) and is split accordingly; the caller-abandon
reconciliation is designed ONCE before any of its consumers ship;
owner-side channel-OBJECT finalization (the 048 residual, distinct
from 060's handle box) is pulled into scope; the non-copy gate task
enumerates every buffered-payload ownership location found in the
runtime read; the strict-zero observable becomes a named probe
matrix; the 052 residual-arm-shape closure is carved out of the 058
lane into its own design-backed task. One review claim was rejected
after lead verification: `crossingRecordExecutable` exists
(`internal/buildpipeline/crossing_transport.go:79`); the citation
stands. A draft-v1 citation error found by the review is fixed:
RV2-DEBT-039 is CLOSED (owned FmtArg) and is no longer listed as an
out-of-scope leak tail.

## Why This Epic Exists

Epic 19 made local reclamation real (scope-exit synthesis, recursive
composite glue). Epic 20 activated the crossing side for shipped
STATE: compiled drop functions with real ids at every publish site,
the publication-accepted handoff contract, and drop-count censuses
across every refusal/abandon/stale/cancel edge for all four crossing
families. The closeout bench showed exact alloc==free balance on
heap-bearing crossing programs and -44% RSS.

What remains leaking is exactly the class this vertical is named
for: values whose free cannot ride a local scope exit because the
OWNER of the obligation is on the other side of an edge —

- the reply edge: a completed body's owned RESULT when the awaiting
  caller abandons it (RV2-DEBT-053, two independent discard paths:
  one owner-side, one caller-side);
- the abandon point: a cancelled-before-first-poll task's frame pair
  that `rewriteAsyncReturns` deliberately keeps alive and nothing
  ever drains (RV2-DEBT-059);
- the handle edge: the caller-side `far Channel<T>` handle box —
  `far Task<T>` retires on `.await()` consumption, but a channel has
  NO consuming operation, so the handle needs drop glue that the
  pre-arc channel model never had (RV2-DEBT-060);
- the channel object itself: runtime-backed Channel values
  (`rt_channel_new`) have no drop glue either — the recorded
  RV2-DEBT-048 residual ("out of the drop arc's current scope" at
  the time), a leak DISTINCT from the handle box and shared by
  local channels; handle glue alone cannot reach strict zero.

The same missing machinery is what keeps the ChannelCreate gate
Copy-only: non-copy payloads through far channels need abandon-time
reconciliation of the select commit bit (Task 7's design d2) and
owner-routed reclamation of values parked in cross-shard buffers at
close/teardown. Opening that gate is the arc's capability payoff —
"cross-shard send of `own` shard-movable values" from the
`RUNTIME_V2.md` capability table.

Closing these edges also finishes the census story: the Epic 20
strict census proved the migration vertical at true zero but had to
pin share/select at exact documented bounds (the 060 handle leak)
and the caller-cancel e2e at safe-and-bounded (the 059 pair). This
epic's observable is a named probe matrix reaching strict zero —
plus retiring the census confounds (057/058) that force probe
authors to dodge whole program shapes.

## Starting State (evidence; Epic 20 closeout + codex code reads)

- **RV2-DEBT-053 is TWO defects (code-confirmed):**
  (a) OWNER-SIDE release-while-DONE — `dispatch_release`
  (`rt_remote_task_dispatch.c`) sees TASK_DONE and releases the
  owner task; `free_task` never inspects `task->result_bits`, so a
  heap-carried result leaks permanently. This path never touches
  the caller-side sweep — caller-side fixes do NOTHING for it; it
  needs result-drop metadata plus a transfer-state check in
  `free_task` (or an equivalent owner-side finalizer).
  (b) CALLER-SIDE orphaned pending — a landed reply strands in the
  caller-side `rt_remote_task_pending.result_bits` opaque frame slot
  if the caller cancels before its next poll. `caller_task_id` is a
  DEAD FIELD for AWAIT/CANCEL while EXECUTE/EXECUTE_ANCHORED/
  CHANNEL_SELECT set it and get the teardown sweep
  (`rt_immediate_on_release_owned`, filter confirmed by direct
  read). Fix: identity + widened sweep (single-shot discipline
  mirroring `consume_handle`) + pending-result disposal. The
  rejected caller-side frame-drop sweep stays rejected (the
  RV2-DEBT-051 opaque-pointer hazard class).
- **RV2-DEBT-059 (abandoned frames):** `spawn body(); cancel; await`
  with any internal await leaks one init state+payload pair per
  occurrence (32+24B local, 72B far shape), zero without the cancel.
  Cancelled frames MUST stay alive until the deferred abandon point
  (the runtime may re-park/re-enter while scope children drain) —
  the defect is that no abandon-time drain exists, not the deferral
  itself. Cancel-before-first-poll is reliably reachable at 2+
  shards with zero yields (Epic 20 empirical). Same abandon-time
  family as 053(b) — one reconciliation design must cover both.
- **RV2-DEBT-060 + the 048 channel-object residual:**
  `rt_far_channel_release` is fully implemented and NEVER CALLED
  from compiled code. Every `channel_on`/`.share()` leaks its
  caller-side handle box: exactly 1,280B/52 blocks on the
  strict-census program, shard-topology-independent. Additionally
  (codex, code-confirmed): the owner-side `rt_channel` OBJECT has no
  finalization path either — local select censuses already recorded
  the two `rt_channel_new` objects as their residual (DEBT-048 row);
  `rt_channel_close` wakes waiters but does NOT drain the buffer.
  Sema semantics (code-confirmed): `far Channel<T>` is
  affine/move-only in source programs — `.share()` is the sanctioned
  duplication path (it adds a lease to the SAME registry entry, it
  does not copy buffered payloads) — so drop glue needs no refcount
  for valid programs. HAZARD found by the review:
  `rt_far_channel_handle_drop` frees the raw handle allocation even
  when the lease is stale, so any compiler/runtime-internal
  duplicate pointer to one physical handle box is a double-free —
  the glue work carries an explicit exactly-once proof obligation.
  The registry's reclaim predicate (entry frees only at zero active
  leases AND zero in-flight pins) is already the row-proven
  pin-balance observable from Epic 20 Task 6.
- **Task 7 execution finding (2026-07-20): local `Channel<T>` object
  lifecycle is OUT OF REACH for this task, and that's fine — the FAR
  side already has everything it needs.** `core/intrinsics.sg`
  declares `Channel<T>` `@copy`; `isDroppableType`
  (`internal/sema/drop_obligations.go`) excludes Copy types before
  anything else, so a LOCAL (non-crossing) `own Channel<T>` binding
  never gets a scope-exit drop obligation synthesized at all — this
  is not a "leaf-gated, missing backend case" like the far handle;
  there is no obligation to hook into. Confirmed empirically:
  `let sender_ch = ch; let recv_ch = ch;` (the golden
  `vm_async_j7_buffered.sg` shape) relies on Copy semantics to hand
  one channel to two independent task closures with no ownership
  tracking, and `struct rt_channel` carries no refcount field —
  `rt_channel_close` only sets a flag and wakes waiters, never frees.
  Making local channels droppable would mean adding refcounting to a
  type kept Copy on purpose for this exact fan-out convenience, or
  making it affine like `far Channel<T>` with its own `.share()`
  equivalent — either is a language-semantics change breaking working
  programs, not a backend/runtime task. SCOPED OUT of Task 7;
  recorded as its own residual below. The FAR side is unaffected by
  any of this and is fully achievable: `rt_far_channel_dispatch_create`
  (`rt_far_channel.c`) mints the registry entry as the OWNER-SIDE
  `rt_channel` object's only reference from creation on (confirmed by
  reading the mint call site directly — nothing else holds
  `entry->channel`), so the SAME `active_leases`/`inflight` predicate
  that already reclaims the registry entry safely reclaims the
  channel object too, at the same choke point (`release_entry`).
- **RV2-DEBT-048's LOCAL scope, now precisely bounded:** the residual
  this row already named ("runtime-backed Channel values have no drop
  glue") is broader than the far-handle gap RV2-DEBT-060 covers — it
  is EVERY channel, local or far, because `rt_channel_new`
  (`rt_async_channel.c`) is a bare `rt_alloc` with no `rt_free` call
  anywhere in the runtime for it, confirmed by grep. Task 7 closes
  the far half completely (handle box + owner object). The local half
  stays open under this row, now scoped precisely to "local
  `Channel<T>` needs either refcounting or an affine redesign before
  it can be droppable at all" — a design question for a future epic,
  not a Task 7 gap.
- **Task 7 deferral (design d2):** the select winner commit is
  already atomic and authoritative (one-lock critical section covers
  cancel check + arm-scan delivery; kind=1/bits=K IS the commit bit;
  row-proven under forced interleaving). What is missing is
  caller-side CONSUMPTION of that bit for drop obligations: gate the
  cancelled caller's deferred drop on reply resolution, so the
  abandoned caller skips exactly the committed arm. Observable only
  with non-copy far channels; MUST land in the same change as
  opening the ChannelCreate gate (`crossingRecordExecutable`,
  ChannelCreate case — citation lead-verified). Sema symmetry is
  already done (far arms get SEM3140/3141 + ArmDrops, Epic 20
  Task 7 row 1).
- **Non-copy buffered-payload ownership locations (codex runtime
  read; the Task 8 enumeration baseline):** channel buffer entries
  are raw `uint64_t` with NO payload-drop metadata — channel
  creation takes only a capacity, there is no path today to thread a
  payload destructor into the owner-side channel; parked-sender
  resume slots hold undelivered payloads; `select_arms[].send_bits`
  hold uncommitted send payloads; refusal/shutdown/control-lane
  paths release message envelopes but know nothing of payload
  ownership; credit control is message/lane-based, not
  payload-aware; last-lease retirement can run while payloads are
  still buffered (share() aliases the entry, so this is the real
  teardown risk, not lease copies).
- **Census confounds (dispositions owed):** RV2-DEBT-057 —
  `emitUnionCast` leaks the source box on re-tagging casts; showed
  up as +1 block/iter on EVERY bench program incl. the local
  control (the explicitly-typed `TaskResult` let shape).
  RV2-DEBT-058 — compare-arm payload bindings never reach
  `registerDroppableBinding`; masked by the ubiquitous moving shape
  `Success(x) => x`; also the recorded reason the 052 residual arm
  shapes (mixed payloads, whole-value binding) had to err toward
  leaking. RV2-DEBT-054 — while-body droppable `let`s leak per
  iteration (the loop back-edge is not an emission point).
  RV2-DEBT-055 — `for _ in` fails MIR validation (small normalize
  fix; the discarded-payload release branch is dead code until
  then). RV2-DEBT-056 — struct-element array boxes never reclaim.

## Fixed Points

- No new surface syntax. `own`, `channel_on`, `.share()`, send/recv,
  select are unchanged; opening the ChannelCreate gate lifts a sema
  restriction on existing syntax.
- Transport invariants unchanged: generation-token discipline, the
  publication-accepted handoff, the single final-release drop site,
  refusal-before-pending drops, retry `(id=0, state=null)`.
- Double-drop stays structurally impossible: every new obligation
  (result, frame pair, handle box, channel object, buffered payload)
  moves exactly once, with a named owner at every point in its
  lifetime. For the handle box this is an explicit proof obligation:
  one physical box, one emitted drop, across moves, async-frame
  persistence, returns, and error paths.
- The caller-abandon reconciliation is designed ONCE (Task 6) and
  every consumer — the landed-reply disposal, the frame-pair drain,
  and later the d2 commit-bit consumption — rides that one
  mechanism. No task builds a private teardown path.
- **Acceptance bar per edge (carried from Epic 20):** BOTH a
  deterministic edge-forcing row with a dispatch-hit/count assertion
  AND a compiled heap-census row; census rows carry execution
  witnesses (RV2-DEBT-049); censuses use runtime-built payloads,
  never string literals.
- The epic's observable is a NAMED PROBE MATRIX at strict zero, not
  a blanket claim: per crossing vertical (migration / share /
  select / non-copy channel) x per edge class (happy path, cancel,
  refusal, teardown-with-buffered-payloads), with the excluded
  payload classes stated (heap bignums, literal-reparse loops,
  arbitrary-precision floats — the 035/036/038 tails); channel-
  OBJECT reclamation is censused separately from handle/lease
  reclamation so a failure names which owner leaked. The
  caller-cancel e2e tightens from safe-and-bounded to zero.
- Frees keep using today's global `malloc`/`free`: freeing on a
  non-allocating shard is correct now. Allocator Phase 5 (shard
  pools, remote-free queues) consumes the recorded seam LATER; this
  epic completes the ownership edges that make routing meaningful
  (see fork 1).
- Established task rules apply unchanged: expand only the next task,
  test-first rows, per-task gates incl. committed-tree Sentrux
  comparisons (baseline rebaselined 2026-07-20), `make check` before
  completion, behavior-named identifiers only, gatecheck wiring for
  every new tagged test.

## Forks (leanings recorded; design review resolves)

1. **Epic scope vs allocator Phase 5.** The arc named vertical 3
   "owner-routed frees", and `RUNTIME_V2.md` Phase 5 is the
   allocator half (owner-shard metadata, remote-free queues, drained
   at safepoints, shard-local pools). LEANING: this epic ships the
   OWNERSHIP EDGES (053/059/060/048-residual/d2) and
   records/extends the two Phase-5 site families
   (obligation-transfer sites, actual free sites) — the allocator
   routing itself stays a successor epic. Rationale: the edges are
   provable today with global free and are prerequisites for
   routing to mean anything; pool design wants its own proving
   spike and bench plan. The review may pull a Phase-5 spike task
   in if the closeout bench shows cross-thread frees becoming
   measurable.
2. **053 split (was "two-step fix" — reframed on codex evidence).**
   053(a) owner-side and 053(b) caller-side are INDEPENDENT defects
   with disjoint code paths and separate fixes; the ledger row stays
   one row, but the epic runs them as separate tasks. LEANING:
   053(a) = result-drop metadata (`result_drop_fn_id` threaded like
   the state's `BodyFuncID`) + a transfer-state check on the
   owner-side release path; 053(b) = caller identity
   (`caller_task_id` for AWAIT/CANCEL) + widened sweep +
   pending-result disposal, riding the Task 6 reconciliation
   design. Open sub-question for review: whether 053(a)'s check
   lives in `free_task` or a dedicated owner-side finalizer.
3. **One caller-abandon mechanism, designed first.** LEANING
   (upgraded from v1 per the review): the abandon-time
   reconciliation point is DESIGNED before either consumer ships —
   one mechanism drains the orphaned landed reply (053b) and the
   kept-alive frame pair (059), and exposes the seam d2 will
   consume in the gate task. Separate rows and separate close
   conditions per debt stay, so a partial fix cannot silently close
   the other row.
4. **Channel lifecycle shape: drop glue for the handle AND
   owner-side finalization for the object.** The v1 refcount
   sub-question is ANSWERED (affine/move-only confirmed in sema;
   `.share()` is the sanctioned duplication and aliases the
   registry entry): no refcount for valid programs. LEANING: the
   `far Channel<T>` handle box becomes a droppable leaf in the
   Epic 19 glue family — binding death releases the handle exactly
   once (`rt_far_channel_release` gains its compiled caller); no
   `close()` surface operation. NEW in v2: owner-side channel
   finalization is in scope — the `rt_channel` object frees when
   its last owner lets go (far: registry entry retirement; local:
   the binding's drop glue), including buffer draining, because
   handle glue alone cannot reach strict zero and `rt_channel_close`
   does not drain. The exactly-once physical-box proof obligation
   (Fixed Points) guards the known `rt_far_channel_handle_drop`
   double-free hazard.
5. **Non-copy ChannelCreate opening: in-epic, last, against the
   enumerated ownership map.** LEANING: in-epic — it is the arc's
   payoff and the deferral's landing slot — but strictly AFTER the
   reconciliation design (fork 3) and the channel lifecycle
   (fork 4). v2 upgrade: the gate CANNOT open before a payload
   destructor path exists into the owner-side channel (buffer
   entries are raw bits today), and the task must prove ownership
   at every enumerated location: buffer entries, parked-sender
   resume slots, `select_arms[].send_bits`, refusal/shutdown/
   control-lane paths, credit-stall parking, and last-lease
   retirement with a non-empty buffer. The d2 reconciliation and
   the non-copy e2e rows land in the SAME change as the gate
   opening. If the review judges the remaining risk too wide, the
   recorded fallback is to close the epic at the probe matrix
   minus the non-copy column and re-charter the gate opening — the
   deferral text then moves whole, not half-consumed.
6. **054-058 dispositions.** LEANING: 057 and 058 are in-epic
   burndown tasks — 057 poisons every census window (+1 blk/iter on
   the local control) and 058 is the prerequisite for the 052
   residual shapes; both are prerequisites for the probe matrix
   being honest. 055 is a small normalize fix — in-epic, any slot.
   The 052 residual-arm-shape closure is its OWN design-backed task
   (v2 change: it reopens scrutinee-box ownership vs extracted
   bindings across branch scopes and suspension — the hard part of
   052, with the guard-UAF history as a warning sign — and must not
   ride as a trailing clause of a lane). 054 and 056 are local
   drop-emission-family gaps — one parallel lane task IF a short
   recon confirms they share an emission root; otherwise split,
   with re-ledgering to a drop-emission maintenance epic as the
   fallback disposition.

## Planned Tasks

Dependency edges (everything else may overlap):
`T2 → T3`; `T6 (design row) → T6 (impl) and → T8`; `T7 → T8`;
`T9 last`. T1, T2, T4, T5 are parallel lanes with no inbound edges
(T5 is owner-side-only and touches nothing the caller-abandon design
owns). All census rows carry execution witnesses.

- **Task 1 — Census-confound burndown (parallel lane): COMPLETE
  2026-07-20.** RV2-DEBT-057 CLOSED (commit 925cf534): `emitUnionCast`
  frees the source box exactly once, guarded by the function's
  existing `isRefType` ownership check; negative-control-verified;
  new gate row `TestRuntimeV2DropUnionCastReclamation` (single-window
  exact free-count + n=1/n=500 loop differential). The heap-capture
  census residual constants (d=1, b=3) were NOT re-attributed by this
  fix — confirmed unchanged, so they are a different cause (left open,
  candidate: RV2-DEBT-058's class). RV2-DEBT-055 CLOSED (commit
  8b8ca4cd): `normalizeIterFor` falls back to the iterable's own
  element type (`iterableElementType`: ArrayInfo / ArrayFixedInfo /
  single-type-arg struct instance) when the loop pattern binds no
  symbol; negative-control-verified on both backends; new probe
  `array_for_discard_n_times` added to the existing iterator-protocol
  census (n=1/n=2000 differential) exercises the discarded-payload
  release branch, confirmed no leak.
- **Task 2 — Compare-arm binding obligations (parallel lane):**
  RV2-DEBT-058 registration ONLY — arm payload bindings reach
  `registerDroppableBinding`; an arm-bound heap payload consumed
  locally reclaims at arm end; census rows pin it. The 052 residual
  shapes are Task 3, not this lane.
- **Task 3 — 052 residual arm shapes (design-backed, needs T2):**
  mixed-payload and whole-value-binding arms release through the
  binding instead of leaking. Reopens scrutinee-box ownership vs
  extracted bindings across branch scopes and suspension — a design
  row precedes code, with the 052 guard-fallthrough UAF history as
  the adversarial checklist; census rows pin the previously-leaking
  shapes at zero.
- **Task 4 — Local drop-emission family (parallel lane, fork 6):**
  recon row FIRST: confirm RV2-DEBT-054 (while-body per-iteration
  reclaim; back-edge or block-end emission point) and RV2-DEBT-056
  (struct-element array boxes) share an emission root; if not,
  split and re-scope with the review's fallback. Rows: 054 —
  d(N)-independence, the RV2-DEBT-040 census rows drop their
  declare-outside workaround note; 056 — the struct[] iterator
  probe asserts strict zero instead of differential cancellation.
- **Task 5 — Owned results, owner side (053a; parallel lane,
  fork 2):** `result_drop_fn_id` threaded like the state's id
  (payload type known at the crossing lowering site) + the
  transfer-state check on the owner-side release path
  (release-while-DONE frees a heap-carried result exactly once,
  never a consumed one). Rows: release-while-DONE dispatch-hit +
  census; Copy-result negative control (inert bits, no drop call);
  consumed-result negative control (no double drop with the
  compiled `finish_retry` consume).
- **Task 6 — Caller-abandon reconciliation: design, then drain
  (053b + 059, forks 2-3):** the design row lands FIRST and names
  the single abandon-time reconciliation point, its ownership
  states, and the seam d2 consumes (Task 8 depends on this row,
  not on the impl).

  **DESIGN ROW v1 REFUTED IN PART, v2 IN PROGRESS (2026-07-20).**
  v1 (below in spirit, not verbatim — superseded) proposed reusing
  `rt_immediate_on`'s EXECUTE-shaped bound/unbound sweep for
  AWAIT/CANCEL and anchoring 059 at `TermAsyncReturnCancelled`. An
  adversarial codex review (independent pass, then lead-verified by
  direct code read against every cited line — all confirmed) found
  BOTH anchor points wrong: 053b's reuse actively mis-routes and
  would leak the far task on top of the leak it was fixing; 059's
  terminator is real but unreachable from the actual leak's own
  path. This is exactly the kind of gap the design-review step exists
  to catch, and it worked. v2 below replaces the two mechanisms; the
  reconciliation point and the d2 seam stand unchanged.

  **The reconciliation point (unchanged).** `mark_done`
  (`runtime/native/rt_task_complete.c`, immediately before
  `task->state = NULL`) is the established single point where a
  task's ownership sweeps run: `rt_far_task_release_owned` and
  `rt_immediate_on_release_owned` both fire there today, on every
  completion path. 059's drain hooks in here; 053b's fix turns out
  NOT to need this hook at all (see below) — its leak site is a
  different, more specific function reached independently of
  `mark_done`.

  **053b v2 (orphaned landed reply) — simpler than v1, and
  code-confirmed sufficient without touching the owner side at
  all.** Lead-traced the exact leak site directly: `dispatch_reply`
  (`rt_remote_task_dispatch.c`) is what the caller-side reply
  message ultimately reaches. Its own comment says the quiet part
  out loud — "an orphaned reply — the caller already resumed
  through the cancel path and dropped its reference — must not
  leave a freed pending linked in the list" — it calls
  `rt_remote_task_pending_finish` (writes `result_bits`/status) then
  `rt_remote_task_pending_consume` (unlink + release). If THIS
  release is what drops the pending's LAST ref (because the caller
  side already released its own ref earlier, exactly the abandon
  case), the pending frees RIGHT HERE, inside `dispatch_reply`,
  before any compiled code ever gets a chance to read `result_bits`
  — that IS the leak, precisely. `rt_remote_task_pending_new` starts
  refs at 1 (the caller's own `*pending` slot) and `start_remote_task`
  immediately adds a second ref (the in-flight-request ref that
  `dispatch_reply`'s own consume drops on arrival) — two refs,
  cleanly separable, confirmed by reading both allocation sites.
  v1's error was inventing owner-side cancel-routing work that
  isn't needed: REJECTED, code-confirmed unsafe (`consume_handle`
  is a one-shot OPEN→state CAS; the original `.await()` already
  moved it to `HANDLE_AWAIT`, so a routed cancel's CAS fails →
  bogus CONSUMED reply to the caller, `owner_registered` cleared
  under a still-live registration, `dispatch_execute_cancel`'s safe
  diversion is EXECUTE-family-only so AWAIT/CANCEL falls through to
  the generic path and the far task's ref leaks). v2 needs NO
  owner-side interaction:
  1. Populate `caller_task_id` in `start_remote_task`
     (`rt_remote_task_api.c`, currently never set for AWAIT/CANCEL —
     confirmed by reading `rt_remote_task_pending_new`), so a sweep
     can find these pendings by caller.
  2. A caller-teardown sweep for AWAIT/CANCEL pendings does exactly
     ONE thing: `rt_remote_task_pending_consume(pending)` — drop the
     caller's own ref, single-shot (clear `caller_task_id`), nothing
     else. No cancel routed, no owner-side state touched.
     IMPLEMENTATION CORRECTION (found by the harness rows, not by
     inspection): the draft above initially reached for plain
     `..._release`, reasoning that staying registry-linked let the
     pending "survive" for the not-yet-landed case. That reasoning
     was backwards — if this release IS the last ref (the landed
     case), `..._release` frees the struct while it is STILL LINKED
     into the registry, leaving a dangling pointer for the next
     registry walk to dereference (a real double-free, caught by the
     new negative-control rows before commit). `..._consume` (unlink,
     then release) is correct unconditionally: unlinking is always
     safe, because nothing else ever finds a pending through the
     registry scan by `caller_task_id` once this sweep has cleared
     it, and `dispatch_reply` finds its pending through the pointer
     the message itself carries, never through the registry list —
     matching `dispatch_reply`'s own existing code comment, which
     already says a landed reply "must not leave a freed pending
     linked in the list" for exactly this reason. If the reply hasn't
     landed yet, the pending survives (unlinked, still alive) on its
     remaining in-flight ref and resolves normally later via
     `dispatch_reply` exactly as today.
  3. `result_drop_fn_id` on `rt_remote_task_pending` (v1's design,
     unchanged): mirrors 053a's `task->result_drop_fn_id`, threaded
     from the AWAIT/select crossing lowering site, cleared wherever
     compiled code actually consumes `result_bits` (the `finish_retry`
     success path). `rt_remote_task_pending_release`'s free path
     (the SAME function whether reached via `dispatch_reply`'s consume
     or the new caller-teardown sweep's consume) gains the drop call.
     One free path, two possible callers, exactly-once by
     construction.
  **053b IMPLEMENTED 2026-07-20.** Landed as its own small function
  (`rt_remote_task_release_owned`, `rt_remote_task_api.c`) rather than
  widening `rt_immediate_on_release_owned` — clearer against the
  EXECUTE-family branches' bound/unbound logic, which this sweep
  deliberately does not share. Five deterministic harness rows
  (`internal/vm/testdata/remote_task_behavior_caller_abandon.c`, run
  through the existing `TestRuntimeV2RemoteTaskBehavior` table, gated
  by `runtime-v2-transport-check`): drops-landed-result-exactly-once,
  copy-inert negative control, consumed negative control, an
  op+caller filter row (proves an EXECUTE-op pending and a different
  caller's AWAIT pending are both left untouched), and an
  in-flight-survives row. The filter and consumed rows caught the
  release-vs-consume bug above before commit.

  **059 v2 (abandoned init frame) — relocated to the actual leak
  site.** v1 anchored at `TermAsyncReturnCancelled`
  (`rewriteAsyncReturns`, `async_codegen.go`). Code-confirmed: that
  terminator is emitted ONLY by the scope-join cancellation check
  (`async_lowering_state_machine.go` ~254-256, `Return{Cancelled:
  true}` — the ONLY call site in the whole `internal/mir` tree that
  sets `Cancelled: true`), reached only after a body's own
  `JoinAll` reports cancellation. The documented 059 repro
  (`TestRuntimeV2FarTaskCallerCancel`'s "cancelled before its own
  checkpoint ever ran" shape) never reaches it: cancellation at an
  ordinary suspend point is detected inside `rt_async_yield`
  (`rt_async_poll.c`), which does `poll_result.state = state;` then,
  on `current_task_cancelled`, sets `POLL_DONE_CANCELLED` and
  `longjmp`s straight out of the poll call — a runtime bail-out, not
  a compiled terminator. Lead-verified directly (`rt_async_poll.c`
  ~301-306). The stashed box IS `state` at that exact `rt_async_yield`
  call — a single value (the ledger's "32B direct + 24B indirect" is
  one composite box plus something it points to, not two independent
  allocations; confirmed against `buildAsyncPendingBlocks`,
  `async_codegen.go` ~146-149, which frees the INCOMING resumed
  state at the START of a poll, before the NEW suspend-point state
  is built — the box at risk is the freshly-built one about to be
  handed to `rt_async_yield`, not the old one, which is already gone
  by then). v1's fear of "re-entry reaching the success terminator
  and double-freeing the stash" was also refuted, harmlessly: once
  `apply_poll_outcome` sees `POLL_DONE_CANCELLED`, `poll_task`'s
  `task->cancel_pending` short-circuit (`rt_async_poll.c` ~115-140)
  means compiled code is NEVER invoked again for this task — no
  second cancelled terminator, no reachable success terminator,
  ever. Lead-verified directly. Design v2: capture the abandon
  pointer where the box is actually alive and about to be lost —
  inside `rt_async_yield`'s cancelled branch, before the `longjmp`.
  This needs the compiler to make the drop information available at
  that call (the state's type — hence its shallow free shape — is
  compile-time-known at the SAME call site the same way
  `emitAsyncStateFreeIntrinsic` already knows it for the success
  path); the mechanical shape (an added `rt_async_yield` parameter
  vs. a `task`-level field set by compiled code immediately before
  the call) is NOT yet settled and is the one open implementation
  question for this task, not a correctness risk — either shape
  gives `mark_done` a nonzero id/pointer pair to consume exactly
  once, cleared after. No re-entry hazard exists to guard against
  (confirmed above), which simplifies this considerably relative to
  v1's assumed clear-on-every-re-entry-path burden.

  **Neighborhood hazard (unchanged, sharper now).** `dispatch_reply`,
  `rt_immediate_on_finish_retry`, and `rt_remote_task_pending_release`
  are exactly where RV2-DEBT-061 lives. The review additionally
  produced a concrete, lead-verified root-cause LEAD for 061 (not a
  fix): `rt_immediate_on.c`'s publication-accepted handoff writes
  `pending->state_owned = 0` (line ~271) AFTER releasing
  `state->lock` (unlocked at line ~254) — a plain, unsynchronized
  write — while `rt_remote_task_pending_release`'s free path reads
  `state_owned`/`body_state` with no lock at all. A caller-side
  release racing the owner's handoff has no synchronization between
  them: exactly matches both 061 failure shapes (a stale-read double
  drop of `body_state`, or a write landing in memory the racing
  release just freed). Filed as evidence on the RV2-DEBT-061 ledger
  row for whoever picks that investigation up; NOT this task's job
  to fix. Given 053b v2 no longer touches `rt_immediate_on.c`'s
  handoff path at all (no owner-side interaction), 053b is now LOW
  risk for 061 entanglement. 059's `TestRuntimeV2FarTaskCallerCancel`
  proof still drives real `on`/`spawn on distributed` traffic through
  061's neighborhood at 1/2/8 shards — do NOT tighten that
  multi-shard census to strict zero until 061 is independently fixed
  (an intermittent 061 hit would be indistinguishable from a 059
  regression in that same test). Prove 059 first on a minimal,
  local-only, deterministic (sync-point-forced, not loop-probability)
  repro that never touches `rt_immediate_on`/far-task dispatch at
  all; keep the existing far-task e2e at its current bounded-not-zero
  assertion with an explicit comment carving out the 061 risk,
  rather than silently absorbing an unrelated race into this task's
  own pass/fail signal.

  **The d2 seam (unchanged).** Whatever caller-teardown sweep 053b's
  point 2 lands as is the abandon-time reconciliation point Task 8's
  design d2 needs — gating a cancelled far-select caller's deferred
  drop on reply resolution is additive scope on this same mechanism,
  not a new one.

  **Narrow follow-up (2026-07-20), both items resolved, no new
  gaps found:**
  1. **`dispatch_reply` refcount completeness — CONFIRMED.**
     Exhaustive grep of every `rt_remote_task_pending_add_ref`/
     `_release`/`_consume` call site in `runtime/native/`, traced
     per op-kind. An AWAIT/CANCEL pending carries exactly two refs
     (creation in `rt_remote_task_pending_new`, plus
     `start_remote_task`'s own `add_ref`), released by exactly two
     mutually-exclusive-where-relevant paths each: the "in-flight"
     ref by whichever of {`dispatch_reply` on normal delivery,
     `rt_remote_task_release_msg_payload` on shutdown-drain} applies
     (`rt_remote_task_completion.c` confirms AWAIT/CANCEL message
     kinds are covered there too); the "caller-slot" ref by whichever
     of {the caller's own `finish_retry` consume,
     the new caller-teardown sweep's release} applies. No third path
     touches either ref for these two ops. The 053b v2 design stands
     as specified.
  2. **`rt_async_yield` stash mechanism — RESOLVED, and it changes
     the free shape.** `rt_async_yield` has exactly ONE emission site
     in the entire backend (`emitTermAsyncYield`,
     `internal/backend/llvm/emit_async.go`) — adding parameters is a
     single-site change, not a sprawl across every suspend point.
     But: the follow-up also found the design's assumed free shape
     was wrong. `emitAsyncStateFreeIntrinsic`'s shallow
     `rt_free(ptr,size,align)` is safe on the SUCCESS path
     specifically because that path runs AFTER the state's fields
     have already been unpacked into separately-drop-obligated
     locals (own comment: "the payload's fields were already
     unpacked... only the boxes themselves are dead") — ownership of
     any nested heap-owned field has already moved out. The state
     value `emitTermAsyncYield` hands to `rt_async_yield` is the
     OPPOSITE: freshly packed by the variant constructor
     (`buildAsyncPendingBlocks`, `async_codegen.go` ~151-159) for the
     NEXT poll to unpack — nothing has been transferred out of it
     yet. A shallow free here would silently leak any nested
     heap-owned field the state box points to (exactly the ledger's
     "24B indirect" component, most likely). Fix: use a RECURSIVE
     drop, the same composite drop-glue every other owned value in
     this codebase gets (`requireDropGlue`/`__surge_drop_call`,
     `emit_drop_glue.go`), not a flat size/align free. This is v1's
     original "mirror 053a's dispatch idiom" instinct, correctly
     anchored this time: `emitTermAsyncYield` already has
     `term.AsyncYield.State`'s exact compile-time type, calls
     `requireDropGlue` on it there, and passes the resulting id
     alongside the state pointer as new `rt_async_yield` arguments,
     stashed onto `task` for `mark_done` to consume via
     `__surge_drop_call`.

  Both follow-ups clean. Proceeding to implementation.

  Then: implement both fixes,
  `TestRuntimeV2FarTaskCallerCancel`'s LOCAL/deterministic 059 row
  tightens from safe-and-bounded to ZERO (the bounded-multiple
  assertion is deleted, not relaxed) while its far-task/multi-shard
  row stays bounded pending RV2-DEBT-061; separate close conditions
  per debt.

  **059 IMPLEMENTED 2026-07-20, two findings changed the plan.**
  Second-opinion design consult (Codex, cross-checked against a direct
  code trace) confirmed the task-level-stash mechanism is safe (no
  re-entry, no double-consume) and additionally found: (a)
  `rt_async_return_cancelled` (the scope-join/`TermAsyncReturnCancelled`
  twin of `rt_async_yield`) needed the identical fix — same leak class,
  reached via a different terminator; both are now covered, both new
  `rt_task` fields (`abandoned_state`/`abandoned_state_drop_fn_id`) set
  once at either origin, consumed once in `mark_done`, cleared after; (b)
  `run_ready_one` bypasses `apply_poll_outcome` entirely in single-worker
  mode, a separate structured-concurrency correctness gap — filed as
  RV2-DEBT-063, not fixed here. Dispatch mechanism differs from the
  original sketch: the state box is ALWAYS heap-allocated (unlike a task
  RESULT, which may be inert Copy bits with no box at all), so gating on
  `typeOwnsHeap` before registering would skip freeing the box itself for
  a no-heap-fields state — confirmed empirically (an unconditionally-
  registered id via `registerCrossingDropResult` produced an EMPTY glue
  body for such a state, leaking the box). Fixed with a THIRD, dedicated
  TypeID-keyed dispatch, `__surge_drop_abandoned_state_call`
  (`registerAbandonedStateDrop`/`emitAbandonedStateDropDispatch`,
  `emit_drop_glue.go`/`emit_async.go`), routing to the SAME composite
  box-freeing glue (`dropGlueName`/`emitDropGlueBody`) the FuncID-keyed
  crossing-state mechanism already uses — that glue unconditionally frees
  the box after any field recursion, which the result mechanism's
  `emitDropValue`-based glue does not.

  `TestRuntimeV2FarTaskCallerCancel`'s LOCAL row plan (tighten to
  zero) did NOT hold: a NEW, unrelated-to-059 gap surfaced while
  proving it (RV2-DEBT-062 — the recursive composite drop-glue walk
  silently skips handle-shaped types, Task<T>/Channel<T> confirmed,
  nested as a field/element; they need their own release call, not a
  raw `rt_free`, and `typeOwnsHeapRec`'s default case never routes to
  one). A git-stash A/B on the row's own minimal repro proved 059's
  real effect precisely: 32B direct + 24B indirect before, 24B direct
  + 0 indirect after — the state box itself (32B) is gone; the
  remaining 24B is one abandoned `Task<T>` local from
  `checkpoint().await()`, RV2-DEBT-062's residual, not 059's. Added
  `TestRuntimeV2CancelledSuspendStateReclaimed`
  (`internal/vm/runtime_v2_cancelled_suspend_state_e2e_test.go`) as the
  local/deterministic proof instead, with a bounded (not zero)
  assertion documenting exactly this split. The existing far-task
  census (`runFarTaskCallerCancelValgrindCensus`) also measures
  differently now — its per-occurrence unit dropped from 40B to a
  clean 32B (0 indirect), because `t.await()`'s crossing consumes the
  far-task handle before suspending, so no Task<T>-shaped local
  survives into that particular payload; the remaining 32B is
  plausibly RV2-DEBT-061, not diagnosed further, per that debt row's
  own "do not tighten to strict zero" guidance. Comment and constant
  updated to match; assertion structure (bounded multiple, not zero)
  intentionally unchanged.
- **Task 7 — Far-channel lifecycle: handle glue + owner-object
  finalization (060 + the 048 residual's FAR half, fork 4; local
  scope removed 2026-07-20, see the execution finding above):**
  `rt_far_channel_release` gains its compiled caller via the glue
  family (new `isFarChannelType` leaf case in `emitInstrDrop`
  dispatching to `rt_far_channel_handle_drop`); a handle binding's
  death releases exactly once — with the exactly-once physical-box
  rows (moves, async-frame persistence, returns, error paths)
  guarding the known `rt_far_channel_handle_drop` double-free hazard;
  sibling leases retire with their entries (registry reclaim
  predicate hits zero). Owner-side: the `rt_channel` OBJECT frees at
  the SAME choke point (`release_entry`) once the registry's existing
  `active_leases`/`inflight` predicate hits zero — a single
  `rt_free` of the struct+buffer block (`rt_channel_free`), since
  `rt_channel_new` allocates both in one block. HONEST SCOPE (codex
  double-checked, 2026-07-20): this reclaims the channel OBJECT for
  any element type, but does NOT drain buffered-but-unreceived
  payloads — buffer entries are raw bits with no destructor path
  (fork 5's own finding). For `Channel<int>`/fixnum T (every row this
  task's programs use; ChannelCreate is Copy-only until fork 5 opens
  it) this is a complete fix, no payload can leak. A heap-carried
  payload still sitting in the buffer at retirement would still leak
  alongside it — not a regression (it leaked before too, as part of
  the whole unreclaimed struct), but the closeout text must say
  "channel object reclaimed; buffered-payload destruction is fork 5's
  non-copy work," never "fully reclaimed for all T." Local `Channel<T>`
  object finalization is OUT OF SCOPE (language-semantics question,
  not this task's — see the execution finding above; forks 4-5
  already anticipated this exact contingency, so no new DEBT row) and
  the local-select census's two-object
  residual is UNCHANGED by this task. Share/select census tiers
  tighten from documented bounds toward zero at 1, 2, and 8 shards
  for the FAR handle+object class specifically.
  **COMPLETE 2026-07-20.** Both fixes landed exactly where fork 4
  anticipated them: `isFarChannelType` (new helper,
  `internal/backend/llvm/emit_channel.go`) gates a new case in
  `emitInstrDrop` (`emit_instr.go`, NOT `emitDropValue` — an
  attempted composite-field-drop addition there was tried, found to
  race an unrelated pre-existing bug during investigation, and
  reverted; the top-level scope-exit path was always the right and
  sufficient hook), dispatching to `rt_far_channel_handle_drop`. A
  new `rt_channel_free` (`runtime/native/rt_async_channel.c`) is
  called from `release_entry` (`rt_far_channel.c`) once
  `active_leases==0 && inflight==0`. New gate row
  `TestRuntimeV2DropFarChannelHandleAndObjectValgrindZero`: strict
  zero at 1/2/8 shards (create + four independent `.share()` leases
  + scope exit; deliberately avoids `on ch {...}` send/recv — see
  below). Existing census tests updated to their new, lower,
  measured values (not re-derived): `TestRuntimeV2CrossingStrictCensusBalanced`'s
  share/select growth figures (11→7, 67→35, 14→9, 91→51, matching
  exactly 1 fewer unit per `.share()` call × call count × iteration
  count) and `TestRuntimeV2CrossingStrictCensusValgrindBounded`'s
  documented bound (1,280B/52blk → 344B/13blk) — both still
  nonzero, pinned to the KNOWN, deferred lease-struct residual (each
  `.share()`'s `rt_far_channel_lease` record accumulates until the
  entry's own last lease releases, which these programs' own channel
  bindings do not always reach within the measured window or by
  process exit — not the handle box or channel object this task
  targets).
  **BYCATCH: RV2-DEBT-061 opened.** Building this task's fuller,
  send/recv-exercising reproducer surfaced a PRE-EXISTING, unrelated,
  intermittent (~10-25% of runs) invalid-free/invalid-write under
  valgrind in the immediate-on/anchored retry path
  (`rt_immediate_on_finish_retry`/`rt_remote_task_pending_release`) —
  confirmed present on unmodified pre-Epic-21 HEAD at a matching
  rate, confirmed absent from every one of this task's own code
  paths (neither the far-channel drop fix nor the reverted
  composite-field attempt changed the rate). Not fixed here — the
  gate test above avoids `on ch {...}` specifically to stay
  race-free; RV2-DEBT-061 needs its own root-cause investigation.
- **Task 8 — Non-copy far channels + commit-bit reconciliation
  (the Task 7 deferral, fork 5; needs T6 design + T7):** first a
  payload-destructor path into the owner-side channel (creation
  carries payload-drop metadata; buffer entries stop being raw
  bits). Then open the ChannelCreate gate AND land design d2 in
  the same change: the cancelled caller's deferred drop gates on
  reply resolution via the T6 seam. Ownership proven at every
  enumerated location: buffer entries, parked-sender resume slots,
  `select_arms[].send_bits`, refusal/shutdown/control-lane paths,
  credit-stall parking, last-lease retirement with a non-empty
  buffer. Non-copy e2e rows: winner not double-dropped, losers
  reclaimed once, abandoned caller skips exactly the committed
  arm; teardown-with-buffered-payloads census; the Epic 20
  Copy-payload race rows rerun unchanged as regression guards.

  **Task 8 design v1 (2026-07-20), under review before implementation.**
  Direct code read confirmed all four enumerated ownership sites still
  hold exactly as described, with concrete locations:

  1. **Buffer entries** (`rt_channel.buf`, `runtime/native/rt_channel_lane.h`):
     `rt_channel` carries no payload type info at all. `rt_channel_free`
     (`rt_async_channel.c`) is a bare `rt_free`, no drain.
  2. **Owner-object teardown** (`release_entry`, `rt_far_channel.c`):
     calls `rt_channel_free` at the same choke point RV2-DEBT-060 already
     proved exactly-once — the drain just needs to happen INSIDE that
     call, not a new choke point.
  3. **Parked-sender resume slot** (`task->resume_bits`,
     `rt_async_channel.c`/`rt_channel_sync.c`): a sender parked waiting
     for buffer space or a direct rendezvous holds its value in
     `resume_bits` with `resume_kind == RESUME_NONE` until delivery sets
     `resume_kind = RESUME_CHAN_SEND_ACK` and clears `resume_bits` to 0.
     OPEN QUESTION (not resolved by this read): `RESUME_NONE` is also
     the default state for a task that has never touched a channel at
     all — `resume_kind == RESUME_NONE && resume_bits != 0` is the only
     signal that distinguishes "parked sender holding an undelivered
     non-copy value" from "ordinary task, nothing to drop," and I have
     not yet traced whether that combination is reachable from any OTHER
     state a task can be cancelled in. Needs verification, not assumed.
  4. **`select_arms[].send_bits`** (`rt_remote_task_pending`,
     `rt_far_channel_select.c`): `rt_remote_task_pending_release`'s free
     path already frees the `select_arms` ARRAY (`rt_free` on the
     block) but never touches individual entries' `send_bits`. The
     commit is already atomic and identifiable (`kind=1/bits=K` per the
     Task 7 deferral note); design d2 is precisely: the free path must
     know which arm (if any) committed, so it drops every OTHER send
     arm's payload exactly once and skips the committed one (already
     delivered/consumed elsewhere).

  **Proposed mechanism (draft, not yet implemented):**
  - Caller compile site (`channel_on::<T>(...)`, the `ChannelCreate`
    crossing lowering) computes a `payload_drop_fn_id` from `T` the same
    way 053a computed `resultDropID` — `registerCrossingDropResult`
    gated on `typeOwnsHeap(T)`, 0 for Copy T.
  - Threaded across the (same-process, pointer-payload, not
    serialized — confirmed by reading `rt_far_channel_create`/
    `rt_far_channel_dispatch_create`, so no wire-format concern) shard
    hop via a NEW `rt_remote_task_pending` field (capacity already
    overloads `body_poll_fn_id`; drop id needs its own field), stored
    onto the owner-side `rt_far_channel_entry` at mint time AND onto
    `rt_channel` itself (buffer drain needs it at `rt_channel_free`
    time; `release_entry` always has the entry in hand when it calls
    free, so either storage location works — TBD which is cleaner).
  - Buffer drain: `rt_channel_free` walks `buf_head..buf_head+buf_len`
    (mod capacity) and calls `__surge_drop_result_call(payload_drop_fn_id,
    (void*)bits)` per live entry before freeing the block — each
    buffered `uint64_t` for a non-copy `T` IS already the value's raw
    pointer bits, the same convention `result_bits`/`abandoned_state`
    already use.
  - Parked-sender resume slot: pending verification above; if reachable,
    needs its own abandon-time check (plausibly in `mark_done`,
    mirroring the abandoned-state-box pattern from RV2-DEBT-059 — a
    task-level drop obligation set when parked, consumed once at
    completion).
  - `select_arms`: `rt_remote_task_pending_release`'s free path gains a
    loop over `select_arms[0..select_count)`, dropping each SEND arm's
    `send_bits` via `payload_drop_fn_id` (per-arm, not per-pending, since
    a select can mix channels of different element types) EXCEPT the
    committed index — needs the pending to record which index committed
    (or derive it from the existing `result_kind`/`result_bits` the
    commit already writes).

  **Not yet done:** an adversarial review pass (mirroring Task 6's
  v1→v2 correction) before any of this is implemented — the resume-slot
  ambiguity above and the select commit-index plumbing are exactly the
  kind of assumption that refuted 053b v1 and 059 v1 on inspection.
  Proceeding to review, not implementation, next.

  **Task 8 design v1 REFUTED (2026-07-20), by adversarial review
  (codex) before any code was written.** Verdict on each item:

  1. **Buffer drain** — sound as designed, one addition: it must run
     under the same quiescence guarantee `release_entry` already
     provides (`rt_far_channel.c`, the RV2-DEBT-060 choke point), not
     inside `rt_channel_free` standalone (`rt_channel_free` today takes
     no lock).
  2. **Parked-sender resume slot — the v1 claim was WRONG, not just
     unverified.** `resume_kind == RESUME_NONE && resume_bits != 0` is
     NOT sender-park-specific: it is the state of ANY task immediately
     after attempting a send, successful or not (`rt_channel_send_inner`
     sets exactly this pair before delivery, `rt_async_channel.c`
     ~109-110, and a completed send returns without clearing it). There
     is no existing disambiguator between "parked sender holding an
     undelivered value" and "task that just finished sending
     successfully." Worse than the v1 framing: `mark_done`
     (`rt_task_complete.c`) never reads `resume_bits`/`resume_kind` at
     all — an undelivered non-copy send value is SILENTLY LOST on
     cancellation today, already, for any task in this window (invisible
     today only because every payload is Copy).
  3. **`select_arms[].send_bits` — the "kind=1/bits=K IS the commit bit"
     claim (borrowed from the Task 7 deferral note) is UNSAFE.** No
     field on `rt_remote_task_pending` records which arm committed
     after the fact; the winner index lives only as a stack local
     inside the commit's own critical section
     (`rt_far_channel_select.c`) and becomes the generic task result.
     Worse: a shutdown/cancel-inflight sweep
     (`rt_remote_task_wait.c` ~46-54) unconditionally overwrites
     `result_kind=2/result_bits=0` on ANY still-pending entry regardless
     of op — erasing the very field an inference scheme would need.
     `rt_remote_task_pending_release`'s free path only ever touches the
     generic `result_bits`/`result_drop_fn_id`, never per-arm
     `send_bits`.
  4. **Transport payload** — confirmed exactly as designed: in-process
     pointer via `rt_transport_msg.payload`, no serialization, no
     wire-format work needed for a new drop-fn-id field.
  5. **TWO ownership sites the v1 enumeration missed entirely:**
     (a) a parked RECEIVER's delivered-but-unconsumed value — send-side
     delivery writes `RESUME_CHAN_RECV_VALUE` + bits straight into the
     receiver's `resume_bits`; if that receiver is cancelled before its
     next poll, the identical `mark_done` gap loses it — the mirror
     image of item 2 on the recv side; (b) the pre-pending allocation-
     failure path in `rt_far_channel_select.c` (~100-123): if
     `rt_remote_task_pending_new` fails AFTER `arms[]` is already
     populated with live SEND `send_bits`, that failure path does a
     bare `rt_free` on the arm array with zero per-arm drops — a "drop
     loop lives in `rt_remote_task_pending_release`" design cannot reach
     this path at all, since no pending exists yet.

  **v2 direction (not yet implemented):** replace the inferred
  `resume_kind`/`resume_bits` signal with an EXPLICIT, unambiguous
  task-level drop obligation for BOTH the send and recv mailbox
  (mirroring the `abandoned_state`/`abandoned_state_drop_fn_id` pattern
  RV2-DEBT-059 already added to `rt_task`: set once at park/delivery,
  consumed exactly once by `mark_done`). Add an immutable
  `select_committed_index` (sentinel = no-commit) to
  `rt_remote_task_pending`, written once at the select commit and
  EXCLUDED from whatever the shutdown/cancel sweep in
  `rt_remote_task_wait.c` overwrites, plus per-arm drop-fn bookkeeping
  used at every free site — including the pre-pending allocation-failure
  path found above, not just `rt_remote_task_pending_release`. This is a
  materially larger design surface than v1 assumed (new task-level
  fields for two mailboxes, not one; a new pending field with a
  shutdown-sweep exclusion; a free-site audit beyond the one choke point
  v1 targeted) — narrow verification on each of these three additions
  individually before implementing, same discipline as 053b/059.

  **Narrow verification (2026-07-20), one review recommendation
  corrected.**

  1. **Send mailbox — the review's proposed new task-level field is
     WRONG, would double-drop.** Direct trace of `emitInstrChanSend`
     (`internal/backend/llvm/emit_channel.go`) plus the async suspend
     rewriting (`buildAsyncPendingBlocks`, `internal/mir/async_codegen.go`):
     a parked send's value is an ordinary live local at the `InstrChanSend`
     instruction (`async_liveness.go` marks `ChanSend.Value` as a use
     there); on resume, the pc-dispatch jumps back to the SAME block and
     RE-EXECUTES the instruction with the value reconstructed from the
     packed suspend state — meaning the value is ALREADY protected by
     RV2-DEBT-059's `abandoned_state` mechanism when a parked sender is
     cancelled. `task->resume_bits` briefly mirrors the same bits inside
     `rt_channel_send_inner`'s attempt loop, but only as a transient
     scratch value for the "was my last attempt already ACKed" check —
     not an independent ownership location. EMPIRICALLY CONFIRMED: a
     local (non-far) `Channel<string>` probe — sender parks on a
     zero-capacity channel holding a live heap string, caller cancels the
     sender before its first poll — leaked ONLY the channel object
     (40B, matching the already-known RV2-DEBT-062 handle-in-composite
     gap), zero bytes attributable to the string. Adding a
     `resume_bits`-keyed drop obligation for the send mailbox would drop
     the same string a second time. Do NOT add it.
  2. **Recv mailbox — review's finding CONFIRMED, and the fix is
     simpler than "add a task-level field."** `rt_channel_recv`
     (`runtime/native/rt_async_channel.c` ~190-194) unconditionally
     clears `resume_kind`/`resume_bits` to `RESUME_NONE`/0 the moment it
     sees the calling task cancelled — with NO check for whether
     `resume_kind == RESUME_CHAN_RECV_VALUE` (a value a sender already
     delivered into this exact mailbox, injected externally by the
     sender's own delivery call, bypassing the receiver's packed suspend
     state entirely — genuinely NOT covered by RV2-DEBT-059). No new
     `rt_task` field needed: `rt_channel` already has `ch` in scope at
     this call site, and Task 8's own planned `payload_drop_fn_id` field
     on `rt_channel` (for buffer draining) is exactly what this site
     needs too — drop `task->resume_bits` via `ch->payload_drop_fn_id`
     when `resume_kind == RESUME_CHAN_RECV_VALUE`, before clearing.
  3. **`select_committed_index` — CONFIRMED necessary, and the second
     found free-site is real.** Traced the full lifecycle:
     `rt_far_channel_select` (caller side, `rt_far_channel_select.c`
     ~44-140) allocates `arms[]` with live `send_bits` populated BEFORE
     `rt_remote_task_pending_new` is even called (~100-111); the
     `request == NULL` failure branch (~118-124) frees `arms` with a bare
     `rt_free`, exactly as the review found — confirmed directly, not
     assumed. On the winning side, `rt_anchored_channel_select`
     (owner-shard body, same file, ~277-314) determines `winner` as a
     plain local and returns it; the compiled anchored body
     (`emitCrossingSelectCancelledOrWinner`-ish, actually
     `internal/backend/llvm/emit_crossing_select.go` ~150-170) consumes
     `winnerBits` for its own dispatch but NEVER touches `arms[]`/
     `send_bits` at all — confirmed by reading the emission directly,
     no clearing, no marking. There is no implicit "already consumed"
     sentinel to lean on (a 0 `send_bits` isn't reliably distinguishable
     from a genuine already-live value some other way). An explicit
     `select_committed_index` field on `rt_remote_task_pending`
     (sentinel = no-commit, e.g. `UINT64_MAX` or count-out-of-range) is
     the only sound way to make the info survive to `rt_remote_task_pending_release`'s
     free path — matches the review's recommendation, now independently
     confirmed rather than taken on trust.

  **Consolidated v2 direction:** one new `rt_channel` field
  (`payload_drop_fn_id`, threaded from the caller's `channel_on::<T>`
  compile site through the in-process pending message, same as v1) used
  at THREE sites: buffer drain in `rt_channel_free` (v1, unchanged),
  the `rt_channel_recv` cancellation early-return (new, corrects the
  review's over-broad "two new task fields" suggestion down to zero new
  task fields), and per-arm select drops keyed by the NEW
  `select_committed_index` on `rt_remote_task_pending`, applied at BOTH
  free sites (`rt_remote_task_pending_release` AND the pre-pending
  allocation-failure path in `rt_far_channel_select.c`). No new
  `rt_task` fields at all — smaller surface than either v1 or the
  review's v2 recommendation. Ready to implement.

  **Task 8 IMPLEMENTED 2026-07-20, gate opened, one fork deferred.**
  `payload_drop_fn_id` (`rt_channel`, threaded from the caller's
  `channel_on::<T>`/`make_channel::<T>` compile sites via
  `registerCrossingDropResult` gated on `typeOwnsHeap`, mirroring 053a's
  `resultDropID` pattern) now drains the buffer at `rt_channel_free`
  (inherits `release_entry`'s existing quiescence guarantee — no new
  lock) and reclaims a sender-delivered-but-uncollected value at
  `rt_channel_recv`'s cancellation early-return (a gap RV2-DEBT-059's
  own mechanism can't see, since the value lands via a PEER's delivery
  call, bypassing this task's own suspend-state entirely). The
  `ChannelCreate` crossing guard is open (`crossingRecordExecutable`
  AND `classifyCrossingPayload` — both needed; the first is the actual
  executable-or-not gate, the second only the diagnostic wording, a
  distinction the first attempt at opening this missed and had to
  correct via a second fix). Proof: `TestRuntimeV2FarChannelNonCopyRoundTrip`
  (create/send/recv a non-Copy `far Channel<string>`, valgrind-pinned to
  exactly the pre-existing, Copy-and-non-Copy-agnostic 8B/1blk channel-
  object baseline — confirmed via A/B with a Copy `int` element on the
  identical shape, not this task's residual to fix). Every existing
  Copy-payload channel/select row reruns clean as a regression guard
  (`TestRuntimeV2DropSelectSendArm`, `TestRuntimeV2ChannelGenesis*`,
  `TestRuntimeV2OnChAnchoredOpsAcrossShards`,
  `TestRuntimeV2DropFarChannelHandleAndObjectValgrindZero`).

  **Select fork deferred — RV2-DEBT-064, found by this task's own e2e
  attempt.** `select_committed_index` and the per-arm
  `payload_drop_fn_id` (`rt_far_channel_select_arm`) were implemented
  per the design above and independently verified CORRECT by direct
  instrumentation (traced winner/loser indices and the skip/drop
  decision at the free site — exactly right every time). The e2e proof
  around them still crashes deterministically (`double free detected in
  tcache 2`), root-caused to a genuinely separate, pre-existing bug in
  `emitChannelSelectCrossing`: unlike `emitChannelCreateCrossing`'s
  deliberate init/retry block split, the select crossing has ONE call
  site shared by the true first attempt and every retry, so a SEND
  arm's owned payload expression gets re-evaluated (rebuilt, then
  silently discarded by the C function's own retry branch) on every
  retry — invisible until now because every existing select arm was
  Copy. Filed as RV2-DEBT-064 with the full trace; not fixed here — it
  touches the shared select lowering (Copy and non-Copy arms both) and
  needs its own narrow verification, not a rushed addition to an
  already-large change. The non-copy select e2e row was written, hit
  this, and was removed rather than left permanently red; whoever picks
  up RV2-DEBT-064 has a working `select_committed_index` mechanism
  waiting for it.

  **Select fork CLOSED 2026-07-23 — RV2-DEBT-064 fixed, and its recorded
  mechanism corrected.** The crash was real and reproduced immediately;
  the cause was NOT the retry re-evaluation the row blamed. Valgrind
  captured both frees and their order: the runtime drops every
  non-committed arm in `rt_remote_task_pending_release`'s `select_arms`
  loop, and compiled code then drops the SAME pointer again through
  sema's per-arm drop synthesis in the winning arm's block
  (`internal/sema/type_expr_select.go` — the moved-set union shared
  verbatim with `compare` and with LOCAL select, no far-specific
  branch). Two owners on the normal path. The fix hands the arm table's
  ownership back to the caller exactly when a winner reply reaches
  compiled code (`select_finish_retry`/`select_disown_arms`,
  `rt_far_channel_select.c`), mirroring the `result_drop_fn_id`
  clear-on-consume idiom but GATED where that one is unconditional — an
  arm payload stays alive on a Cancelled reply, and the compiled
  cancelled path re-enters the pend edge without running any arm block,
  so an unconditional clear would leak. The `select_committed_index`
  mechanism Task 8 left waiting was correct and is now load-bearing on
  the abandon path exactly as designed.

  **The init/retry split is also real, but only for CONST operands —
  and that took an adversarial review to establish.** Three first probes
  (a marker-printing Copy payload in a send arm; the same through a
  select with no ready arm so it genuinely parks and resumes through a
  real retry round; and a lease-minting arm RECEIVER `b.share().recv()`)
  all measured byte-identical with and without the split, and the split
  was briefly reverted as unfalsifiable. The probe set had a HOLE: all
  three are PLACE operands, which `splitAsyncAwaits` temps into a
  preceding block, so a resumed retry only re-loads them. A CONST
  operand is embedded in the crossing instruction and lowers INLINE in
  the crossing block — and a bignum literal allocates right there.
  Measured A/B: `72 bytes/2 blocks` without the split versus `36/1`
  with it, leak stack through `rt_bigint_from_literal`. The split is
  kept, with the accurate mechanism in its code comment so the next
  reader does not repeat the place-operand probe and revert it again.
  Method note worth carrying: a negative control that cannot fail is
  not a negative control — the first three probes all "passed" while
  proving nothing, and only enumerating the operand FORMS (place vs
  const) found the one that discriminates.

  Proof rows: `TestRuntimeV2FarSelectConstArmEvaluatedOnce` pins the
  split (one un-reclaimed bigint, not two — the single remaining block
  is a separate pre-existing gap: `typeOwnsHeap` is false for a bignum
  int, so a delivered bignum payload gets drop-fn id 0 and the
  channel-teardown drain never reclaims it; stated, not fixed here).
  `TestRuntimeV2FarSelectNonCopySendArm` — three far
  channels, a winning SEND arm buffered then reclaimed at channel
  teardown, a losing SEND arm reclaimed by the winning arm's compiled
  drop; exit-code rows at 1/2/8 shards plus a valgrind row at STRICT
  ZERO. It deliberately avoids `on ch {...}` so RV2-DEBT-061's
  intermittent race cannot be confused for this row's subject.
  HARNESS GOTCHA worth carrying forward: the exit-code rows are not the
  gate — glibc's tcache double-free detector is probabilistic, and the
  pre-fix binary passed them outright on some runs while valgrind
  reported the invalid free on that same binary.

  **Two adjacent finds, both repaired here.** (1) The LLVM collect pass
  never collected `Crossing.RemoteOps[k].Value` (`emit_collect.go`),
  although the local `InstrSelect` case collects `arm.Value` right
  beside it — a far-select SEND arm carrying any non-trivial constant
  failed to COMPILE. (2) `runtime-v2-transport-check` was RED at HEAD
  (`db8e10a4`): that commit added `rt_far_channel_select`'s
  `send_drop_fn_ids` parameter without updating the C harness fixture in
  `runtime_v2_remote_publication_test.go`, so five rows failed to build.
  A broken gate hides regressions, so it is repaired as part of this
  work. Residual, pre-existing and NOT from this change: the
  `TestRuntimeV2RemoteTaskBehavior/shutdown-wakes-reply-waiters-on-all-shards`
  row flakes at roughly 1-in-8 on a clean tree as well (measured both
  ways).
- **Task 9 — Bench + debt + closeout:** bench per the Task-1 recipe
  (worktree per commit, release build, time x5, valgrind totals,
  checksum witness) incl. a non-copy channel program; the NAMED
  PROBE MATRIX at strict zero (verticals x edge classes, excluded
  payload classes stated) at `SURGE_SHARDS=1,2,8`; 053/059/060
  closed with evidence and the 048-residual note retired, d2
  deferral consumed, 054-058 dispositions final; Phase-5 seam
  record updated with any new free sites this epic created;
  next-epic scoping (candidates: allocator Phase 5 proper, the
  pre-arc 001-026 ledger revision, the async-entrypoint pair
  049/050 discussion).

## Out Of Scope

- Allocator Phase 5 implementation (shard pools, remote-free
  queues) — fork 1 records the seam duty only.
- RV2-DEBT-049/050 (async-entrypoint execution witness + exit
  code) — separate discussion per the Epic 20 record; harnesses
  keep the sync-main + spawn + compare-await shape.
- Leak tails RV2-DEBT-035/036/038 and the pre-arc 001-026
  revision — unchanged dispositions (probe matrix excludes their
  payload classes explicitly).
- Io boundary / io_uring backends, tier-2 stealing, migration
  control plane — independent tracks.
