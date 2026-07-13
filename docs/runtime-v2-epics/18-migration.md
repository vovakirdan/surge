# Epic 18: Owned-Value Migration — DRAFT

**Status:** draft (2026-07-13); the central fork goes to second opinion
before the boundary decisions freeze.

## Why This Epic Exists

`@shard_movable` is a promise the crossings do not yet keep: an owned
user value captured into `on` / `spawn on` is guarded off the transport
with FUT7020 ("moves owned data across shards, which this vertical does
not ship yet; pass plain-copy data or build the value on the
destination"). Migration is the last candidate from the post-cleanup
list (`16-candidates.md` C) and the last big lie in the crossing
surface: the type system already classifies movability (SEM3168-3172),
the capture machinery already ships the bits, and only the ownership
discipline is missing.

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

## The Central Fork (out for second opinion)

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
consider itself the owner, on every path:

1. body runs to completion (destination drop glue — works today);
2. body cancelled before its first poll;
3. body cancelled mid-suspension (parked on a channel);
4. dispatch refused (body never created — dispatch-side drop);
5. transport queue full (request rolled back — caller-side drop
   restores caller ownership? NO: the capture moved at the call site;
   the rollback path must drop it, the caller's local is already
   moved-from);
6. stale anchor (anchored blocks; answered without a body);
7. caller teardown with the request in flight (release_owned sweep);
8. owner teardown with the body parked;
9. destination shutdown answered at the call site;
10. leak census: every row above asserts the alloc/free balance
    across ALL accounting cells (the cross-cell free is the expected
    shape, the census proves exactly-once globally).

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

1. Kickoff: fork resolution record, evidence re-pin, drop-obligation
   design note (who owns the state struct's fields on each lifecycle
   edge), sema surface design.
2. Runtime vertical: drop-obligation plumbing through the pending
   lifecycle with matrix rows 1-10 (behavior suite, test-first).
3. Sema + guard flip + e2e (build-local-process-remote at
   SHARDS=1/2/8; negative goldens for non-movable field paths).
4. Bench (capture-move cost vs plain-copy baseline) + debt + closeout.
