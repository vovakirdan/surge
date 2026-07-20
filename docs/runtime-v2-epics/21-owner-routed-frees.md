# Epic 21: Owner-Routed Frees (vertical 3 of the reclamation arc)

**Status:** DRAFT — design review before Task 1. Charter source:
`20-tasks/08-bench-census-closeout.md` (the Epic 20 closeout duty).
Arc plan: `19-candidates.md` (local emission → crossing activation →
owner-routed frees). Carries RV2-DEBT-053, 059, 060, the Epic 20
Task 7 vertical-3 deferral (abandon-time commit-bit reconciliation +
non-copy far-channel e2e), and the 054-058 dispositions.

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
  caller abandons it (RV2-DEBT-053, both discard paths);
- the abandon point: a cancelled-before-first-poll task's frame pair
  that `rewriteAsyncReturns` deliberately keeps alive and nothing
  ever drains (RV2-DEBT-059);
- the handle edge: the caller-side `far Channel<T>` handle box —
  `far Task<T>` retires on `.await()` consumption, but a channel has
  NO consuming operation, so the handle needs drop glue that the
  pre-arc channel model never had (RV2-DEBT-060).

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
epic's observable is tightening every one of those tiers to strict
zero — plus retiring the census confounds (057/058) that force
probe authors to dodge whole program shapes.

## Starting State (evidence, verified at the Epic 20 closeout)

- **RV2-DEBT-053 (owned results), traced design on the ledger row:**
  (a) release-while-DONE — `dispatch_release` sees TASK_DONE and
  releases the owner task; `free_task` never inspects
  `task->result_bits`, so a heap-carried result leaks permanently;
  (b) orphaned caller pending — a landed reply strands in the
  caller-side `rt_remote_task_pending.result_bits` opaque frame slot
  if the caller cancels before its next poll. `caller_task_id` is a
  DEAD FIELD for AWAIT/CANCEL while EXECUTE/CHANNEL_SELECT set it
  and get exactly the needed teardown sweep
  (`rt_immediate_on_release_owned`). Preferred fix (codex-verified,
  confirmed by direct read): populate `caller_task_id` for
  AWAIT/CANCEL + widen the sweep's op filter (single-shot discipline
  mirroring `consume_handle`), then thread a `result_drop_fn_id`
  like the state's `BodyFuncID`. A caller-side frame-drop sweep was
  considered and REJECTED (the RV2-DEBT-051 opaque-pointer hazard
  class).
- **RV2-DEBT-059 (abandoned frames):** `spawn body(); cancel; await`
  with any internal await leaks one init state+payload pair per
  occurrence (32+24B local, 72B far shape), zero without the cancel.
  Cancelled frames MUST stay alive until the deferred abandon point
  (the runtime may re-park/re-enter while scope children drain) —
  the defect is that no abandon-time drain exists, not the deferral
  itself. Cancel-before-first-poll is reliably reachable at 2+
  shards with zero yields (Epic 20 empirical).
- **RV2-DEBT-060 (channel handles):** `rt_far_channel_release` is
  fully implemented and NEVER CALLED from compiled code. Every
  `channel_on`/`.share()` leaks its handle box: exactly 1,280B/52
  blocks on the strict-census program, shard-topology-independent.
  A `.share()` additionally leaves the dispatch-side sibling lease
  reachable via the global registry (same pre-arc class). The
  registry's reclaim predicate (entry frees only at zero active
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
  ChannelCreate case). Sema symmetry is already done (far arms get
  SEM3140/3141 + ArmDrops, Epic 20 Task 7 row 1).
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
  (result, frame pair, handle box, buffered payload) moves exactly
  once, with a named owner at every point in its lifetime.
- **Acceptance bar per edge (carried from Epic 20):** BOTH a
  deterministic edge-forcing row with a dispatch-hit/count assertion
  AND a compiled heap-census row; census rows carry execution
  witnesses (RV2-DEBT-049); censuses use runtime-built payloads,
  never string literals.
- The epic's observable: the strict census tightens to TRUE ZERO on
  all three crossing verticals (migration is already there;
  share/select drop their documented-bound tiers) and the
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
   OWNERSHIP EDGES (053/059/060/d2) and records/extends the two
   Phase-5 site families (obligation-transfer sites, actual free
   sites) — the allocator routing itself stays a successor epic.
   Rationale: the edges are provable today with global free and are
   prerequisites for routing to mean anything; pool design wants its
   own proving spike and bench plan. The review may pull a Phase-5
   spike task in if the closeout bench shows cross-thread frees
   becoming measurable.
2. **053 fix shape.** LEANING: the traced two-step design —
   (1) populate `caller_task_id` for AWAIT/CANCEL and widen the
   existing teardown sweep (correctness: no strand), then
   (2) `result_drop_fn_id` threaded like the state's id
   (reclamation: the actual drop). The rejected caller-side
   frame-drop sweep stays rejected. Open sub-question for review:
   does step 1 alone close the release-while-DONE path, or does
   `free_task` also need to consult the result drop id.
3. **059 mechanism: subsumed or separate.** The ledger's leaning is
   that the 053 abandon design should solve or subsume the frame
   drain ("same design family"). LEANING: design them together —
   one abandon-time reconciliation point that drains BOTH the
   orphaned reply and the kept-alive frame pair — but keep separate
   rows and separate close conditions so a partial fix cannot
   silently close the other row.
4. **060 shape: drop glue, not a consuming op.** LEANING: the
   `far Channel<T>` handle box becomes a droppable leaf in the
   Epic 19 glue family — binding death releases the handle exactly
   once (`rt_far_channel_release` gains its compiled caller); no
   `close()` surface operation. Sibling-lease retirement rides the
   registry's existing reclaim predicate. Open sub-question: handle
   copies/re-binds — what the move/borrow discipline for a far
   channel binding is today, and whether glue needs a refcount or
   the binding is already move-only in practice.
5. **Non-copy ChannelCreate opening: in-epic, last.** LEANING:
   in-epic — it is the arc's payoff and the deferral's landing slot
   — but strictly AFTER the abandon machinery (forks 2-3) and the
   handle lifecycle (fork 4), because buffered non-copy payloads at
   close/teardown need both. The d2 reconciliation and the non-copy
   e2e rows (winner not double-dropped, losers reclaimed once,
   abandoned caller skips exactly the committed arm) land in the
   SAME change as the gate opening. If the review judges the
   remaining risk too wide, the recorded fallback is to close the
   epic at strict-zero censuses and re-charter the gate opening —
   the deferral text then moves here whole, not half-consumed.
6. **054-058 dispositions.** LEANING: 057 and 058 are in-epic
   burndown tasks — 057 poisons every census window (+1 blk/iter on
   the local control) and 058 blocks the 052 residual arm shapes;
   both are prerequisites for "strict zero everywhere" being
   honest. 055 is a small normalize fix that unlocks the
   discarded-payload release branch — in-epic, any slot. 054 and
   056 are local drop-emission-family gaps (back-edge emission
   point; struct-element array coverage) — in-epic as one parallel
   lane task IF the review agrees; the fallback disposition is
   re-ledgering them to a drop-emission maintenance epic with the
   census workaround notes staying in place.

## Planned Tasks

Dependency edges (everything else may overlap):
`T4 → T5 → T7`; `T6 → T7`; `T8 last`. T1, T2, T3 are parallel lanes
with no inbound edges. T7 is the gate-opening task and consumes
forks 2-5. All census rows carry execution witnesses.

- **Task 1 — Census-confound burndown (parallel lane):**
  RV2-DEBT-057 — `emitUnionCast` frees the source box exactly once;
  cast-heavy loop census at strict zero; re-attribute or shrink the
  heap-capture census residual constants (d=1, b=3) with evidence.
  RV2-DEBT-055 — `for _ in` element-type fallback; the
  discarded-payload release branch gets its census row.
- **Task 2 — Compare-arm binding obligations (parallel lane):**
  RV2-DEBT-058 — arm payload bindings reach
  `registerDroppableBinding`; an arm-bound heap payload consumed
  locally reclaims at arm end; then REVISIT the 052 residual arm
  shapes (mixed payloads, whole-value binding) so they release
  through the binding instead of leaking; census rows pin both.
- **Task 3 — Local drop-emission family (parallel lane, fork 6):**
  RV2-DEBT-054 — while-body droppable `let`s reclaim per iteration
  (back-edge or block-end emission point), d(N)-independence row;
  the RV2-DEBT-040 census rows drop their declare-outside
  workaround note. RV2-DEBT-056 — struct-element array boxes
  reclaim when the array drops; the struct[] iterator probe asserts
  strict zero instead of differential cancellation.
- **Task 4 — Owned results over the reply edge (RV2-DEBT-053,
  fork 2):** step 1 ownership correctness (caller_task_id for
  AWAIT/CANCEL + widened sweep, single-shot discipline), step 2
  `result_drop_fn_id` reclamation. Rows: release-while-DONE and
  orphaned-landed-reply each get dispatch-hit + census;
  Copy-result negative control (inert bits, no drop call).
- **Task 5 — Deferred-abandon drain (RV2-DEBT-059, fork 3):** the
  abandon-time reconciliation point drains the kept-alive init
  state+payload pair; cancelled-before-first-poll reclaims exactly
  once on local and far shapes; `TestRuntimeV2FarTaskCallerCancel`
  census tightens from safe-and-bounded to ZERO (the bounded-
  multiple assertion is deleted, not relaxed).
- **Task 6 — Far-channel handle drop glue (RV2-DEBT-060, fork 4):**
  `rt_far_channel_release` gains its compiled caller via the glue
  family; a handle binding's death releases exactly once; sibling
  leases retire with their entries (registry reclaim predicate
  hits zero); the share/select strict-census tiers tighten from
  documented bounds to ZERO at 1, 2, and 8 shards.
- **Task 7 — Non-copy far channels + commit-bit reconciliation
  (the Task 7 deferral, fork 5):** open the ChannelCreate gate to
  non-copy shard-movable payloads AND land design d2 in the same
  change: the cancelled caller's deferred drop gates on reply
  resolution; non-copy e2e rows — winner not double-dropped,
  losers reclaimed once, abandoned caller skips exactly the
  committed arm; buffered-payload close/teardown census; the
  Epic 20 Copy-payload race rows rerun unchanged as regression
  guards.
- **Task 8 — Bench + debt + closeout:** bench per the Task-1 recipe
  (worktree per commit, release build, time x5, valgrind totals,
  checksum witness) incl. a non-copy channel program; strict-zero
  census across ALL crossing verticals at `SURGE_SHARDS=1,2,8`;
  053/059/060 closed with evidence, d2 deferral consumed,
  054-058 dispositions final; Phase-5 seam record updated with any
  new free sites this epic created; next-epic scoping (candidates:
  allocator Phase 5 proper, the pre-arc 001-026 ledger revision,
  the async-entrypoint pair 049/050 discussion).

## Out Of Scope

- Allocator Phase 5 implementation (shard pools, remote-free
  queues) — fork 1 records the seam duty only.
- RV2-DEBT-049/050 (async-entrypoint execution witness + exit
  code) — separate discussion per the Epic 20 record; harnesses
  keep the sync-main + spawn + compare-await shape.
- Leak tails RV2-DEBT-035/036/038/039 and the pre-arc 001-026
  revision — unchanged dispositions.
- Io boundary / io_uring backends, tier-2 stealing, migration
  control plane — independent tracks.
