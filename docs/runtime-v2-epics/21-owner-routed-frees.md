# Epic 21: Owner-Routed Frees (vertical 3 of the reclamation arc)

**Status:** IN EXECUTION — Task 1 COMPLETE (2026-07-20, RV2-DEBT-057
+ 055 both closed). Design review concluded 2026-07-20 via a
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
  not on the impl). Then: caller identity (`caller_task_id` for
  AWAIT/CANCEL, single-shot discipline mirroring `consume_handle`)
  + widened teardown sweep + landed-reply disposal (053b); the
  frame-pair drain at the same point (059) —
  cancelled-before-first-poll reclaims exactly once on local and
  far shapes. `TestRuntimeV2FarTaskCallerCancel` census tightens
  from safe-and-bounded to ZERO (the bounded-multiple assertion is
  deleted, not relaxed). Separate close conditions per debt.
- **Task 7 — Channel lifecycle: handle glue + owner-side
  finalization (060 + the 048 residual, fork 4):**
  `rt_far_channel_release` gains its compiled caller via the glue
  family; a handle binding's death releases exactly once — with the
  exactly-once physical-box rows (moves, async-frame persistence,
  returns, error paths) guarding the known
  `rt_far_channel_handle_drop` double-free hazard; sibling leases
  retire with their entries (registry reclaim predicate hits zero).
  Owner-side: the `rt_channel` OBJECT frees at last-owner release —
  far via registry entry retirement, local via binding drop glue —
  incl. Copy-payload buffer draining; the local-select census
  retires its two-object residual. Share/select census tiers
  tighten from documented bounds to ZERO at 1, 2, and 8 shards,
  with channel-object and handle/lease reclamation censused
  SEPARATELY.
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
