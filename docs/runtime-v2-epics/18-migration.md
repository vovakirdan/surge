# Epic 18: Owned-Value Migration — DRAFT

**Status:** COMPLETE (2026-07-13, same day). All five tasks closed —
task index `18-tasks/README.md`. Direction was approved the same day
(Model A; affine terminal-move fixed point and the recorded tails
confirmed explicitly).

## Closeout (2026-07-13)

What shipped: owned `@shard_movable` captures move into `on` /
`spawn on` / `on ch` bodies (the FUT7020 guard arm for them is gone;
far-Task captures and non-copy payloads keep their guards); the
drop-obligation plumbing end to end — `__surge_drop_call` dispatch (the
compiled twin of the poll dispatch), drop metadata + the single
final-release drop site on both pending families, pre-pending refusal
drops on all four caller surfaces, the publish handoff clear — all
row-proven (7 drop rows incl. the corrected queue-full premise, the
bound-cancel handoff row, and two negative controls); e2e green FIRST
RUN at SHARDS=1/2/8; bench: capture move = plain-copy + ~6%.

TWO scope corrections discovered and recorded mid-epic (both narrow
the epic honestly rather than growing it):
1. Surge has no user destructors — drop functions are recursive frees,
   so glue-edge rows are allocator-balance observables (Task 3).
2. The language emits NO drops at all today (`InstrDrop` is a backend
   no-op) — so the guard flip ships under the current memory model
   with no new leak class, and the entire drop-obligation machinery
   stays dormant at id 0 until language-wide drop emission exists:
   RV2-DEBT-034 records the activation list (compiled drop functions,
   glue-edge rows, owned results) on the seams built here.

Sentrux at closeout (committed tree): root 6169 (advisory), internal
6484, runtime 5307, runtime/native 5395 — runtime scopes RECOVERED vs
the kickoff baseline (5297/5384 -> 5307/5395); internal -5 within the
noise band.

Gates at close: make check, behavior suite + deadlock rows x2,
crossing e2e gate (five verticals at SHARDS=1/2/8), compiler package
suites — green twice.

## Why This Epic Exists

`@shard_movable` is a promise the crossings do not yet keep: an owned
user value captured into `on` / `spawn on` is guarded off the transport
with FUT7020 ("moves owned data across shards, which this vertical does
not ship yet; pass plain-copy data or build the value on the
destination"). Migration is the last candidate from the post-cleanup
list (`16-candidates.md` C) and the last big lie in the crossing
surface: the type system already classifies movability (SEM3168-3172) and
the capture machinery already ships the bits. The representation is
sufficient; the missing work is drop-metadata plumbing on the pending
(the request carries `void* state` with NO drop metadata, and the
abandon paths free only the envelope — `rt_remote_spawn_internal.h`,
`rt_remote_spawn_pending.c` consume/release) plus the exactly-once-drop
discipline across every lifecycle edge. Harmless today only because the
transport gate admits no droppable payloads; this epic OPENS that hole
and must close it in the same motion.

## Starting State (evidence)

- The guard: `CrossingCaptureOwnedShardMovable` verdict
  (`internal/sema/on_crossing_capture.go:198`) → FUT7020 in
  `crossing_guard_classify.go` and executability=false in
  `crossing_transport.go` (`crossingRecordExecutable` rejects the
  verdict). Lifting the epic = flipping exactly these three sites
  behind the new discipline.
- Representation is NOT the problem in this runtime: captures already
  ride the crossing state struct (`buildSpawnOnStateStruct`,
  `internal/mir/lower_expr_crossing_spawn_poll.go`) — shared memory
  means an owned capture is a pointer plus a DROP OBLIGATION, not a
  serialization problem. (The wire-format work the candidates note
  feared belongs to a future non-shared-memory transport, recorded as
  the tail.)
- Heap accounting is thread-attributed counters
  (`rt_heap_accounting_cell`: alloc/free counts+bytes per worker/io/
  blocking cell) — cross-cell alloc-here-free-there is already legal
  and counted; no per-object registry exists to migrate. Locality is
  advisory today.
- Drop machinery: owned locals drop through MIR drop glue on scope
  exit; the crossing body task's state struct fields are dropped by
  the body's own drop glue when the body RUNS. Every path where the
  body NEVER runs is where the discipline is missing today.
- The exactly-once precedent: the reply edge (Epics 13-17) proves the
  shape — every failure path answers exactly once. Migration needs the
  twin: every path drops the moved capture exactly once.

## Fork Resolution (second opinion, 2026-07-13): Model A

External review (codex pass converged) resolved the fork decisively
for A, with corrections folded into this draft:

- **A ships**: lifting FUT7020 for `@shard_movable` owned captures IS
  the existing capture/state-struct lowering serving the guard
  message's named need; no new syntax.
- **B recorded as tail**, with softened wording: a standalone
  `move x to placement` has no USEFUL migration semantics in a
  shared-memory runtime without far DATA handles (ownership transfer
  and destructor timing are observable, but there is no locality to
  enforce — accounting cells are thread-attributed counters). The far
  data-handle family is the recorded tail.
- **Row-5 semantic settled: DROP, not un-move.** A move is statically
  terminal (sema marks the source moved-from at the call expression;
  un-move is not expressible without a fallible-move surface — a
  different, heavier design); it matches own-argument failure
  semantics (ownership transfers at the boundary; cleanup disposes);
  and there is no caller-visible rollback to hand a value back to.
  The row's original PREMISE was wrong though: queue-full is not one
  "rollback" — the initial request enqueue refuses synchronously
  source-side while the ACK path drains-and-retries with no rollback
  at all. The matrix below is re-cut on the corrected model.
- **The matrix's organizing axis, stated explicitly: does a live
  remote owner (body/state object) exist at this edge?** Every edge
  where it does NOT, the pending's terminal cleanup owns the drop;
  every edge where it DOES, the destination owns it — and the rows
  prove no edge lets both (double drop) or neither (leak) happen.

## The Original Fork (retained for the record)

What is vertical 1's surface?

- **Model A — capture lift (no new syntax).** Lift FUT7020 for
  `@shard_movable` owned captures into `on` / `spawn on` / `on ch`
  bodies. `own x` moves into the block; the destination body owns it;
  sema's existing use-after-move already polices the source side. The
  WHOLE epic is then the exactly-once-drop matrix across the crossing
  lifecycle (below) plus the accounting story. Smallest honest
  increment; unlocks "build locally, process remotely" — the actual
  user need the guard message names.
- **Model B — `move x to placement` statement.** A standalone
  migration statement. In a shared-memory runtime with thread-
  attributed accounting, a standalone move of a LOCAL value has no
  observable semantics (no far handle to the data exists; locality is
  advisory): it would be a re-accounting hint at best, new syntax for
  a no-op at worst. B becomes meaningful only with `far T` DATA
  handles (far structs — a whole new handle family) or a
  locality-enforcing allocator — both far beyond one epic.
- **Leaning:** A, with B's far-data-handle family recorded as the
  tail. The candidates note already scoped C as "unlocks the
  @shard_movable promise beyond crossings" — but the crossings ARE
  where the promise is broken today.

## The Exactly-Once-Drop Matrix (vertical 1's core, every row test-owned)

A moved capture must be dropped exactly once and exactly one side must
consider itself the owner, on every path. Rows grouped by the
organizing axis (does a live remote owner exist yet?).

No remote owner yet — the pending's terminal cleanup drops:
1. capture/state construction fails before publish;
2. pending allocation failure;
3. invalid/unresolvable placement (answered at the call site);
4. destination shut down before enqueue;
5. initial request enqueue refused (incl. queue-full — the SYNCHRONOUS
   source-side refusal; the ACK lane's drain-and-retry is a different
   site with no rollback);
6. request discarded by shutdown/stale handling before dispatch;
7. stale anchor (anchored blocks; answered without a body);
8. caller teardown with the request in flight (release_owned sweep,
   unbound branch);
9. caller abandons the returned far Task while publish is pending.

Remote owner exists — the destination owns the drop:
10. body runs to completion (destination drop glue);
11. body cancelled before its first poll;
12. body cancelled mid-suspension (parked on a channel);
13. body-task allocation fails destination-side (state arrived, no
    task — dispatch-side drop; the boundary row between the groups);
14. body task created but ready-queue publish fails;
15. body panics/aborts before its first successful poll;
16. ACK-enqueue failure after the body task exists;
17. duplicate/stale ACK after resolution (absorbed, no second drop);
18. owner teardown with the body parked.

Cross-cutting:
19. NESTED owned captures: exactly-once holds recursively for every
    owned field, not just the outer state object;
20. a field destructor panic leaves no double-drop on the remaining
    fields (partial-cleanup row);
21. leak census: every row asserts the alloc/free balance across ALL
    accounting cells (cross-cell free is the expected shape; the
    census proves exactly-once globally).

## Fixed Points

- Affine move semantics at the capture site: the source local is
  moved-from after the call expression evaluates, on every outcome
  including failures (matching own-argument call semantics).
- No new locality promises: accounting cells stay thread-attributed;
  migration transfers the drop obligation, not bytes.
- The reply payload restriction (plain-copy results) is untouched:
  values move IN; results still come back as plain-copy bits. Owned
  RESULTS are a recorded tail (they need the reverse obligation).
- Kindness-first: every remaining restriction (non-movable field
  paths via NonCopyCulpritPath, owned results) names its cause and
  rewrite.

## Candidate Slices (to be re-cut after the fork resolves)

1. Kickoff: evidence re-pin; the drop-metadata design note (the
   pending gains a drop-fn pointer alongside `state`; who owns the
   state struct's fields on each lifecycle edge, by the live-remote-
   owner axis); sema surface design.
2. Runtime vertical A: drop-metadata plumbing + the no-remote-owner
   rows (1-9) — every abandon path in the pending lifecycle destructs
   `state` exactly once (behavior suite, test-first).
3. Runtime vertical B: the remote-owner rows (10-18) + cross-cutting
   rows (19-21, nested captures and the census).
4. Sema + guard flip + e2e (build-local-process-remote at
   SHARDS=1/2/8; negative goldens for non-movable field paths).
5. Bench (capture-move cost vs plain-copy baseline) + debt + closeout
   (owned RESULTS and the far-data-handle family recorded as tails).
